package flareschema

import "testing"

func TestSchemaVersionIsOne(t *testing.T) {
	if SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1 — bumping is a deliberate, schema-doc-updating change", SchemaVersion)
	}
}

func TestErrUnsupportedSchemaVersionMessage(t *testing.T) {
	err := ErrUnsupportedSchemaVersion{Got: 99}
	want := "flareschema: unsupported schema_version 99 (this build supports up to 1)"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}
