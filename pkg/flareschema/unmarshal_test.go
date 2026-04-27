package flareschema

import (
	"encoding/json"
	"errors"
	"testing"
)

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

func TestUnmarshalHappyPath(t *testing.T) {
	in := BuildSampleFlare()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := Unmarshal(b)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.SchemaVersion != SchemaVersion {
		t.Fatalf("got version %d, want %d", out.SchemaVersion, SchemaVersion)
	}
	if out.User.Email != in.User.Email {
		t.Fatalf("got email %q, want %q", out.User.Email, in.User.Email)
	}
}

func TestUnmarshalRejectsForwardVersion(t *testing.T) {
	raw := []byte(`{"schema_version": 99}`)
	_, err := Unmarshal(raw)
	if err == nil {
		t.Fatal("Unmarshal accepted forward version; want ErrUnsupportedSchemaVersion")
	}
	var verr ErrUnsupportedSchemaVersion
	if !errors.As(err, &verr) {
		t.Fatalf("got %T; want ErrUnsupportedSchemaVersion", err)
	}
	if verr.Got != 99 {
		t.Fatalf("verr.Got = %d, want 99", verr.Got)
	}
}

func TestUnmarshalAcceptsOlderVersion(t *testing.T) {
	// SchemaVersion is 1, so this exercises the "0 < SchemaVersion"
	// path without requiring a real legacy doc. The contract is that
	// Unmarshal does NOT reject older payloads — migration is the
	// caller's job.
	raw := []byte(`{"schema_version": 0, "meta": {"schema_version": 0, "graywolf_version": "old"}}`)
	out, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("Unmarshal rejected older version: %v", err)
	}
	if out.SchemaVersion != 0 {
		t.Fatalf("SchemaVersion = %d, want 0 (round-tripped)", out.SchemaVersion)
	}
}

func TestUnmarshalRejectsInvalidJSON(t *testing.T) {
	_, err := Unmarshal([]byte(`{not json`))
	if err == nil {
		t.Fatal("Unmarshal accepted invalid JSON")
	}
	var verr ErrUnsupportedSchemaVersion
	if errors.As(err, &verr) {
		t.Fatalf("got version error for invalid JSON; want a generic decode error")
	}
}
