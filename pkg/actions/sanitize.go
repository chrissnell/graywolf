package actions

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
)

const (
	defaultArgRegex  = `^[A-Za-z0-9,_-]{1,32}$`
	defaultArgMaxLen = 32
)

// ArgSpec is one entry in an Action's arg_schema, decoded from JSON.
type ArgSpec struct {
	Key      string `json:"key"`
	Regex    string `json:"regex,omitempty"`
	MaxLen   int    `json:"max_len,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// BadArgError carries the first offending key for the reply formatter.
type BadArgError struct {
	Key    string
	Reason string
}

func (e *BadArgError) Error() string { return fmt.Sprintf("bad arg: %s (%s)", e.Key, e.Reason) }

func IsBadArgErr(err error) bool {
	var bae *BadArgError
	return errors.As(err, &bae)
}

var (
	regexCacheMu sync.Mutex
	regexCache   = map[string]*regexp.Regexp{}
	defaultRE    = regexp.MustCompile(defaultArgRegex)
)

func compileRegex(pat string) (*regexp.Regexp, error) {
	if pat == "" || pat == defaultArgRegex {
		return defaultRE, nil
	}
	regexCacheMu.Lock()
	defer regexCacheMu.Unlock()
	if re, ok := regexCache[pat]; ok {
		return re, nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, err
	}
	regexCache[pat] = re
	return re, nil
}

// DecodeArgSchemaJSON parses the JSON wire form of an action's
// arg_schema column into the runtime ArgSpec slice. Empty / "[]"
// strings return (nil, nil).
func DecodeArgSchemaJSON(s string) ([]ArgSpec, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var out []ArgSpec
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SanitizeFromMap is the map-keyed counterpart to Sanitize. The
// Actions REST test-fire endpoint hands us a wire map; the classifier
// hands us an ordered slice. Both paths need the same downstream
// schema check, so this helper converts and delegates to Sanitize.
//
// The returned slice is in schema-declared order (not map iteration
// order) so the executor argv stays stable.
func SanitizeFromMap(schema []ArgSpec, args map[string]string) ([]KeyValue, error) {
	raw := make([]KeyValue, 0, len(args))
	for k, v := range args {
		raw = append(raw, KeyValue{Key: k, Value: v})
	}
	return Sanitize(schema, raw)
}

// BadArgKey returns the offending key when err is a *BadArgError,
// otherwise the empty string. Lets callers compose a "bad arg: KEY"
// reply without importing the error type directly.
func BadArgKey(err error) string {
	var bae *BadArgError
	if errors.As(err, &bae) {
		return bae.Key
	}
	return ""
}

// Sanitize validates raw key/value pairs against the schema and
// returns the canonical ordered slice handed to the executor. The
// returned slice preserves the schema order, not the wire order, so
// command argv is stable.
func Sanitize(schema []ArgSpec, raw []KeyValue) ([]KeyValue, error) {
	bySpec := make(map[string]ArgSpec, len(schema))
	for _, a := range schema {
		bySpec[a.Key] = a
	}
	provided := make(map[string]string, len(raw))
	for _, kv := range raw {
		spec, ok := bySpec[kv.Key]
		if !ok {
			return nil, &BadArgError{Key: kv.Key, Reason: "not allowed"}
		}
		maxLen := spec.MaxLen
		if maxLen <= 0 {
			maxLen = defaultArgMaxLen
		}
		if len(kv.Value) > maxLen {
			return nil, &BadArgError{Key: kv.Key, Reason: "too long"}
		}
		re, err := compileRegex(spec.Regex)
		if err != nil {
			return nil, &BadArgError{Key: kv.Key, Reason: "schema regex invalid"}
		}
		if !re.MatchString(kv.Value) {
			return nil, &BadArgError{Key: kv.Key, Reason: "regex"}
		}
		provided[kv.Key] = kv.Value
	}
	out := make([]KeyValue, 0, len(schema))
	for _, spec := range schema {
		v, ok := provided[spec.Key]
		if !ok {
			if spec.Required {
				return nil, &BadArgError{Key: spec.Key, Reason: "missing"}
			}
			continue
		}
		out = append(out, KeyValue{Key: spec.Key, Value: v})
	}
	return out, nil
}
