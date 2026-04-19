// tagify injects a top-level `tags:` array into the swag-generated
// OpenAPI 2.0 spec so Swagger UI and downstream generators can render
// tag groups in a deliberate, curated order instead of alphabetical.
//
// Why this exists: swag v1.16.x silently drops package-level
// `@tag.name` / `@tag.description` general-info directives — verified
// empirically, confirmed against v2.0.0-rc5 as well. The post-
// processing shim below papers over the gap. Revisit if a future swag
// release honors the directives natively.
//
// Usage:
//
//	go run ./pkg/webapi/docs/cmd/tagify --json path/to/swagger.json --yaml path/to/swagger.yaml
//
// The tool rewrites both files in place. It preserves swag's byte-for-
// byte output style (4-space JSON, 2-space YAML, HTML escapes in JSON
// strings) so `make docs-check` sees stable output across runs.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// tagOrder is the curated tag ordering. Add new tags here — any tag
// that appears in a generated operation but isn't in this list gets
// appended alphabetically at the end with no description.
var tagOrder = []tagEntry{
	// --- Configuration --------------------------------------------------
	{"channels", "Radio channel configuration."},
	{"beacons", "Beacon configuration and send-now."},
	{"audio-devices", "Audio input/output device binding."},
	{"kiss", "KISS TCP server interfaces."},
	{"tx-timing", "Per-channel TX timing parameters."},
	{"ptt", "Push-to-talk device configuration."},
	{"digipeater", "Digipeater configuration and rules."},
	{"igate", "Igate configuration, filters, status, and simulation."},
	{"gps", "GPS source configuration."},
	{"position-log", "Position logging configuration."},
	{"agw", "AGW TCP server configuration."},

	// --- Runtime / data -------------------------------------------------
	{"packets", "Received packet history."},
	{"stations", "Station registry derived from received packets."},
	{"position", "Current station position."},
	{"messages", "APRS text messaging: DMs, tactical threads, preferences, and SSE events."},

	// --- Admin / auth / health ------------------------------------------
	{"auth", "Session login, logout, and first-user setup."},
	{"version", "Build version and commit hash."},
	{"status", "Aggregated runtime status for the dashboard."},
	{"health", "Liveness probe for orchestration."},
}

type tagEntry struct {
	Name        string
	Description string
}

func main() {
	var jsonPath, yamlPath string
	var strict bool
	flag.StringVar(&jsonPath, "json", "", "path to swagger.json")
	flag.StringVar(&yamlPath, "yaml", "", "path to swagger.yaml")
	flag.BoolVar(&strict, "strict", false, "exit non-zero on stragglers (tags used in operations but not in tagOrder)")
	flag.Parse()
	if jsonPath == "" || yamlPath == "" {
		log.Fatal("tagify: both --json and --yaml are required")
	}

	usedTags, existingDescs, err := collectTags(jsonPath)
	if err != nil {
		log.Fatalf("tagify: scan tags: %v", err)
	}
	ordered, stragglers := orderedTags(usedTags, existingDescs)
	if len(stragglers) > 0 && strict {
		log.Fatalf("tagify: --strict: unordered tags present (add to tagOrder): %v", stragglers)
	}

	if err := rewriteJSON(jsonPath, ordered); err != nil {
		log.Fatalf("tagify: rewrite json: %v", err)
	}
	if err := rewriteYAML(yamlPath, ordered); err != nil {
		log.Fatalf("tagify: rewrite yaml: %v", err)
	}
}

// collectTags returns the set of tag names referenced by operations in
// the generated spec, plus any pre-existing top-level `tags:` entries
// keyed by name → description. We scan the JSON (faster and
// structurally simpler than the YAML) and assume the YAML is an
// equivalent view of the same data, since swag emits both from a
// single model.
//
// The second return value is forward-compat scaffolding: swag v1.16.x
// emits no top-level `tags:`, so the map is normally empty. When a
// future swag version does emit descriptions, we preserve them instead
// of overwriting.
func collectTags(jsonPath string) (map[string]struct{}, map[string]string, error) {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, nil, err
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Tags []string `json:"tags"`
		} `json:"paths"`
		Tags []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, err
	}
	seen := map[string]struct{}{}
	for _, methods := range doc.Paths {
		for _, op := range methods {
			for _, t := range op.Tags {
				seen[t] = struct{}{}
			}
		}
	}
	existing := map[string]string{}
	for _, t := range doc.Tags {
		if t.Description != "" {
			existing[t.Name] = t.Description
		}
	}
	return seen, existing, nil
}

// orderedTags filters tagOrder to the tags actually used, then appends
// any used tags missing from tagOrder (alphabetized). If swag already
// emitted a description for a tag (via `existing`), that wins over the
// curated description in tagOrder — this is forward-compat for a
// future swag release that honors `@tag.description`. Returns the
// ordered list and the straggler names (used but absent from
// tagOrder), for --strict to act on.
func orderedTags(used map[string]struct{}, existing map[string]string) ([]tagEntry, []string) {
	out := make([]tagEntry, 0, len(used))
	seen := map[string]bool{}
	for _, e := range tagOrder {
		if _, ok := used[e.Name]; ok {
			entry := e
			if desc, ok := existing[e.Name]; ok {
				entry.Description = desc
			}
			out = append(out, entry)
			seen[e.Name] = true
		}
	}
	// Stragglers: tags used but not listed.
	var extra []string
	for t := range used {
		if !seen[t] {
			extra = append(extra, t)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		fmt.Fprintf(os.Stderr, "tagify: warning: unordered tags appended alphabetically: %v\n", extra)
		for _, t := range extra {
			out = append(out, tagEntry{Name: t, Description: existing[t]})
		}
	}
	return out, extra
}

// rewriteJSON reads swag's JSON, decodes the top level into
// RawMessage fields (preserving nested byte shapes), inserts a `tags`
// entry after `info` in a stable position, and re-emits the file with
// swag's 4-space indent and HTML-escape semantics. Nested structures
// are written through untouched. File mode is preserved.
func rewriteJSON(path string, tags []tagEntry) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Decode preserving top-level key order by streaming the object.
	var fields []field
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("expected top-level JSON object")
	}
	for dec.More() {
		kTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := kTok.(string)
		if !ok {
			return fmt.Errorf("non-string key: %v", kTok)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return err
		}
		fields = append(fields, field{key: key, value: v})
	}

	// Drop any existing `tags` entry so we own the slot.
	fields = dropField(fields, "tags")

	tagsJSON, err := encodeTagsJSON(tags)
	if err != nil {
		return err
	}
	fields = insertAfter(fields, "info", field{key: "tags", value: tagsJSON})

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, f := range fields {
		// Key
		buf.WriteString("    ")
		if err := writeJSONString(&buf, f.key); err != nil {
			return err
		}
		buf.WriteString(": ")
		// Re-indent the nested value by 4 spaces to match swag's style.
		if err := writeIndented(&buf, f.value, 4); err != nil {
			return err
		}
		if i < len(fields)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}")

	return os.WriteFile(path, buf.Bytes(), info.Mode().Perm())
}

type field struct {
	key   string
	value json.RawMessage
}

func dropField(fs []field, key string) []field {
	out := fs[:0]
	for _, f := range fs {
		if f.key != key {
			out = append(out, f)
		}
	}
	return out
}

func insertAfter(fs []field, afterKey string, n field) []field {
	for i, f := range fs {
		if f.key == afterKey {
			out := make([]field, 0, len(fs)+1)
			out = append(out, fs[:i+1]...)
			out = append(out, n)
			out = append(out, fs[i+1:]...)
			return out
		}
	}
	// afterKey not found — prepend so the tags array isn't lost.
	return append([]field{n}, fs...)
}

// encodeTagsJSON renders the tag list as compact-per-entry JSON matching
// swag's array-of-objects indentation. We re-run it through the encoder
// pretty printer afterwards so the output integrates cleanly.
func encodeTagsJSON(tags []tagEntry) (json.RawMessage, error) {
	type t struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	arr := make([]t, 0, len(tags))
	for _, e := range tags {
		arr = append(arr, t{Name: e.Name, Description: e.Description})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "    ")
	enc.SetEscapeHTML(true)
	if err := enc.Encode(arr); err != nil {
		return nil, err
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	return json.RawMessage(out), nil
}

// writeJSONString emits a JSON-quoted string using HTML-escape rules
// (same as Go's encoding/json default, which matches swag's output).
func writeJSONString(w *bytes.Buffer, s string) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(s); err != nil {
		return err
	}
	// The encoder appends a trailing newline; strip it.
	b := w.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		w.Truncate(len(b) - 1)
	}
	return nil
}

// writeIndented writes a RawMessage value, re-indenting every internal
// line by indent additional spaces. swag's pretty-printer uses 4-space
// indentation at the top level; nested objects already carry their
// own indentation relative to their parent, so we only need to shift
// continuation lines.
func writeIndented(w *bytes.Buffer, v json.RawMessage, indent int) error {
	// Pretty-reprint the value to normalize whitespace, then indent
	// continuation lines.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, v, "", "    "); err != nil {
		return err
	}
	pad := bytes.Repeat([]byte{' '}, indent)
	lines := bytes.Split(pretty.Bytes(), []byte{'\n'})
	for i, line := range lines {
		if i > 0 {
			w.Write(pad)
		}
		w.Write(line)
		if i < len(lines)-1 {
			w.WriteByte('\n')
		}
	}
	return nil
}

// rewriteYAML loads the YAML as a Node tree, removes any existing
// top-level `tags` entry, inserts a fresh one after `info`, and re-
// emits with 2-space indentation to match swag. File mode is preserved.
func rewriteYAML(path string, tags []tagEntry) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("yaml: not a document")
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return fmt.Errorf("yaml: root is not a mapping")
	}

	// Drop any existing `tags` entry.
	m.Content = removeMapKey(m.Content, "tags")

	// Build the new `tags` entry.
	tagsKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tags"}
	tagsValue := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, e := range tags {
		item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		item.Content = append(item.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "name"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: e.Name},
		)
		if e.Description != "" {
			item.Content = append(item.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "description"},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: e.Description},
			)
		}
		tagsValue.Content = append(tagsValue.Content, item)
	}
	m.Content = insertMapEntryAfter(m.Content, "info", tagsKey, tagsValue)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), info.Mode().Perm())
}

// removeMapKey walks a MappingNode's Content slice and removes the
// (key, value) pair for the given key. Mapping content is laid out as
// alternating scalar key / value pairs.
func removeMapKey(content []*yaml.Node, key string) []*yaml.Node {
	out := make([]*yaml.Node, 0, len(content))
	for i := 0; i < len(content); i += 2 {
		if content[i].Value == key {
			continue
		}
		out = append(out, content[i], content[i+1])
	}
	return out
}

// insertMapEntryAfter inserts (k, v) after the (afterKey, ...) pair.
// If afterKey isn't present, prepend so we never silently drop the
// entry.
func insertMapEntryAfter(content []*yaml.Node, afterKey string, k, v *yaml.Node) []*yaml.Node {
	for i := 0; i < len(content); i += 2 {
		if content[i].Value == afterKey {
			out := make([]*yaml.Node, 0, len(content)+2)
			out = append(out, content[:i+2]...)
			out = append(out, k, v)
			out = append(out, content[i+2:]...)
			return out
		}
	}
	return append([]*yaml.Node{k, v}, content...)
}
