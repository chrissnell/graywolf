// idlint walks pkg/webapi and pkg/webauth, extracts every @ID value
// appearing in a top-level function's doc comment (via the Go AST —
// including `/* */` block comments which a plain grep would miss), and
// asserts that each value is declared as a string constant in
// pkg/webapi/docs/op_ids.go.
//
// The stale grep-based `make docs-lint` target this tool replaces had
// three gaps: it ignored block comments entirely, it matched mid-line
// `@ID` references (anywhere, not just in annotation doc blocks), and
// it had no understanding of Go syntax so stray occurrences in
// unrelated strings or comments would trigger false positives.
//
// Usage:
//
//	go run ./pkg/webapi/docs/cmd/idlint
//
// Exits non-zero with a diagnostic listing every @ID value that isn't
// a registered constant in op_ids.go.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Roots walked for @ID annotations, relative to the Go module root.
var annotationRoots = []string{
	"pkg/webapi",
	"pkg/webauth",
}

// Directories under the roots to skip entirely. pkg/webapi/docs is
// the registry and associated tooling (tagify, idlint); it has no
// handler code, and its own comments mention `@ID` as prose — scanning
// them would produce noise matches. pkg/webapi/docs/gen is swag's
// generated output, same reasoning.
var skipDirs = []string{
	filepath.Join("pkg", "webapi", "docs"),
}

// Path to the registry, relative to the Go module root.
const registryPath = "pkg/webapi/docs/op_ids.go"

// idRe extracts the @ID value. Runs line-by-line against comment text
// with the leading `//` or `/*` markers already stripped by go/ast.
var idRe = regexp.MustCompile(`@ID\s+([A-Za-z0-9_]+)`)

func main() {
	registry, err := loadRegistry(registryPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "idlint: load registry %s: %v\n", registryPath, err)
		os.Exit(1)
	}

	uses, err := collectIDs(annotationRoots)
	if err != nil {
		fmt.Fprintf(os.Stderr, "idlint: scan annotations: %v\n", err)
		os.Exit(1)
	}

	var missing []idUse
	for _, u := range uses {
		if _, ok := registry[u.id]; !ok {
			missing = append(missing, u)
		}
	}
	if len(missing) > 0 {
		for _, m := range missing {
			fmt.Fprintf(os.Stderr, "docs-lint: @ID %s at %s:%d not found as a constant in %s\n",
				m.id, m.file, m.line, registryPath)
		}
		os.Exit(1)
	}
	fmt.Println("docs-lint: every @ID is registered in op_ids.go.")
}

// idUse records a single @ID annotation occurrence for error reporting.
type idUse struct {
	id   string
	file string
	line int
}

// loadRegistry parses op_ids.go and returns a set of every string
// constant value declared in the file. Only string-literal constants
// are considered; typed or computed constants are ignored (there are
// none today, and a future addition would need explicit support).
func loadRegistry(path string) (map[string]struct{}, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				// lit.Value is quoted ("foo"); strip the quotes. Go
				// string literals don't need full unquoting for our
				// purposes — @ID values are restricted to [A-Za-z0-9_]
				// which has no escape sequences.
				s := lit.Value
				if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
					s = s[1 : len(s)-1]
				}
				out[s] = struct{}{}
			}
		}
	}
	return out, nil
}

// collectIDs walks each root, parses every .go file (excluding _test.go),
// and extracts @ID values from the doc comments of top-level function
// declarations. Non-function declarations are skipped — swag only
// parses handler funcs for operation metadata.
func collectIDs(roots []string) ([]idUse, error) {
	var out []idUse
	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				for _, skip := range skipDirs {
					if path == skip {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
			if perr != nil {
				return fmt.Errorf("%s: %w", path, perr)
			}
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Doc == nil {
					continue
				}
				for _, cg := range []*ast.CommentGroup{fd.Doc} {
					for _, c := range cg.List {
						// c.Text includes the `//` or `/* */`
						// markers. Strip them so the regex matches
						// both comment styles uniformly.
						text := stripCommentMarkers(c.Text)
						for _, line := range strings.Split(text, "\n") {
							m := idRe.FindStringSubmatch(line)
							if m == nil {
								continue
							}
							pos := fset.Position(c.Pos())
							out = append(out, idUse{
								id:   m[1],
								file: path,
								line: pos.Line,
							})
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	// Stable output for diagnostics.
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		if out[i].line != out[j].line {
			return out[i].line < out[j].line
		}
		return out[i].id < out[j].id
	})
	return out, nil
}

// stripCommentMarkers removes the `//` line-comment prefix or the
// `/* */` block-comment wrapper from a raw comment text so the body
// can be scanned uniformly. The returned text preserves internal
// newlines for block comments so multi-line `/* ... @ID ... */`
// comments still scan correctly.
func stripCommentMarkers(s string) string {
	switch {
	case strings.HasPrefix(s, "//"):
		return strings.TrimPrefix(s, "//")
	case strings.HasPrefix(s, "/*") && strings.HasSuffix(s, "*/"):
		return s[2 : len(s)-2]
	}
	return s
}
