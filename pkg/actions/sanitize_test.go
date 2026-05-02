package actions

import (
	"testing"
)

func TestSanitizeDefaultRegex(t *testing.T) {
	schema := []ArgSpec{{Key: "room", Required: true}}
	got, err := Sanitize(schema, []KeyValue{{Key: "room", Value: "garage"}})
	if err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	if len(got) != 1 || got[0].Value != "garage" {
		t.Fatalf("bad sanitize: %+v", got)
	}
}

func TestSanitizePerKeyOverride(t *testing.T) {
	schema := []ArgSpec{{Key: "state", Regex: "^(on|off)$", Required: true}}
	if _, err := Sanitize(schema, []KeyValue{{Key: "state", Value: "maybe"}}); err == nil {
		t.Fatal("expected regex reject")
	}
	if _, err := Sanitize(schema, []KeyValue{{Key: "state", Value: "on"}}); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestSanitizeMissingRequired(t *testing.T) {
	schema := []ArgSpec{{Key: "x", Required: true}}
	_, err := Sanitize(schema, nil)
	if err == nil || !IsBadArgErr(err) {
		t.Fatalf("expected bad-arg, got %v", err)
	}
}

func TestSanitizeUndeclaredKeyRejected(t *testing.T) {
	schema := []ArgSpec{{Key: "x"}}
	_, err := Sanitize(schema, []KeyValue{{Key: "y", Value: "z"}})
	if err == nil || !IsBadArgErr(err) {
		t.Fatalf("expected bad-arg, got %v", err)
	}
}

func TestSanitizeMaxLen(t *testing.T) {
	schema := []ArgSpec{{Key: "x", MaxLen: 3}}
	_, err := Sanitize(schema, []KeyValue{{Key: "x", Value: "abcd"}})
	if err == nil {
		t.Fatal("expected max-len reject")
	}
}
