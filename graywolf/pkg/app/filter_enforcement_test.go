package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIgateServerFilterSingleEntryPoint enforces the Phase 2 design
// invariant that the operator's base APRS-IS server filter
// (igCfg.ServerFilter) is read through exactly one entry point:
// buildIgateFilter in wiring.go. Any other read — in a different
// file under pkg/app/, or in a different function within wiring.go
// itself — would allow the composed-filter augmentation of tactical
// callsigns to be silently bypassed.
//
// The check is a structural AST walk (not a regex) so that comments,
// strings, and unrelated type members named "ServerFilter" do not
// trigger false positives. Specifically, it flags every selector
// expression of the form "igCfg.ServerFilter" and locates the
// enclosing *ast.FuncDecl. A read is legal only when both:
//
//   - the file is wiring.go
//   - the enclosing function is buildIgateFilter
//
// The heuristic "igCfg" is the identifier convention used everywhere
// in wiring.go (see wireIGate and reloadIgate). If a future refactor
// renames it, update this test in lockstep — or, better, refactor
// through buildIgateFilter and the rename becomes irrelevant.
func TestIgateServerFilterSingleEntryPoint(t *testing.T) {
	// Resolve the pkg/app directory from this test file's CWD (go
	// test runs with the package dir as CWD).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(cwd, "*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no .go files found in %s", cwd)
	}

	fset := token.NewFileSet()

	type violation struct {
		file    string
		funcName string
		pos     token.Position
	}
	var violations []violation

	for _, path := range files {
		// Skip test files — tests may construct synthetic igCfg values
		// and read ServerFilter for verification; that is fine.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		// Walk each top-level FuncDecl so we can report the enclosing
		// function name for any violation found inside it. (Reads at
		// package scope — e.g. in a var initializer — are reported
		// with funcName == "" and always treated as violations.)
		fileName := filepath.Base(path)
		checkNode := func(funcName string, body ast.Node) {
			if body == nil {
				return
			}
			ast.Inspect(body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel == nil || sel.Sel.Name != "ServerFilter" {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if ident.Name != "igCfg" {
					return true
				}
				// Found a read of igCfg.ServerFilter. Legal only in
				// wiring.go inside buildIgateFilter.
				if fileName == "wiring.go" && funcName == "buildIgateFilter" {
					return true
				}
				violations = append(violations, violation{
					file:    fileName,
					funcName: funcName,
					pos:      fset.Position(sel.Pos()),
				})
				return true
			})
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				// Package-level declarations: still scan for the
				// forbidden selector; funcName is empty.
				checkNode("", decl)
				continue
			}
			checkNode(fn.Name.Name, fn.Body)
		}
	}

	if len(violations) > 0 {
		var b strings.Builder
		b.WriteString("igCfg.ServerFilter must only be read inside buildIgateFilter in wiring.go.\n")
		b.WriteString("The operator's base server filter is augmented with tactical-callsign g/ clauses\n")
		b.WriteString("by buildIgateFilter; any other read bypasses that augmentation.\n")
		b.WriteString("Violations:\n")
		for _, v := range violations {
			fn := v.funcName
			if fn == "" {
				fn = "<package-scope>"
			}
			b.WriteString("  ")
			b.WriteString(v.pos.String())
			b.WriteString("  (in func ")
			b.WriteString(fn)
			b.WriteString(")\n")
		}
		t.Fatal(b.String())
	}
}

// TestIgateServerFilterEnforcementDetectsViolation is a meta-test that
// verifies the enforcement scanner above would actually catch a
// regression. It parses a synthetic source snippet containing a
// forbidden read of igCfg.ServerFilter outside buildIgateFilter and
// asserts the scanner flags it. Without this, a broken scanner would
// silently pass and future refactors could slip through.
func TestIgateServerFilterEnforcementDetectsViolation(t *testing.T) {
	const badSource = `package app

type fakeCfg struct{ ServerFilter string }

func someOtherFunc(igCfg *fakeCfg) string {
	return igCfg.ServerFilter // forbidden: read outside buildIgateFilter
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synthetic.go", badSource, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse synthetic: %v", err)
	}

	found := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel != nil && sel.Sel.Name == "ServerFilter" {
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "igCfg" {
					if fn.Name.Name != "buildIgateFilter" {
						found = true
					}
				}
			}
			return true
		})
	}
	if !found {
		t.Fatal("enforcement scanner failed to detect forbidden igCfg.ServerFilter read in synthetic source")
	}
}
