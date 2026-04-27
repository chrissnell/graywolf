// Command flareschema-gen prints the JSON Schema document for the
// flareschema.Flare struct to stdout. Wired into the top-level
// Makefile's `flareschema` target.
//
// Stable output: invopop/jsonschema walks struct fields in declaration
// order, so re-running the generator against an unchanged Go source
// tree yields a byte-identical document.
//
// Forward-compatibility: AllowAdditionalProperties is enabled so older
// receivers don't reject payloads from newer client builds carrying an
// extra field before the server's migration path runs. The version
// gate (SchemaVersion / ErrUnsupportedSchemaVersion in the schema
// package) is what enforces compatibility, not a strict-properties
// schema check.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/invopop/jsonschema"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

func main() {
	r := &jsonschema.Reflector{
		// Allow additional fields so older client builds emitting an
		// extra (forward-compatible) property don't fail validation
		// before the server's migration path runs.
		AllowAdditionalProperties: true,
		// Inline definitions: the operator UI's generated schema viewer
		// is happier with one self-contained document than with $ref
		// chains.
		ExpandedStruct: false,
	}
	schema := r.Reflect(&flareschema.Flare{})
	out, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "flareschema-gen: marshal: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(append(out, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "flareschema-gen: write: %v\n", err)
		os.Exit(1)
	}
}
