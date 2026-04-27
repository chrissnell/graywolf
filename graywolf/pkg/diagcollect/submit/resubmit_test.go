package submit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildUpdate_AddsChangesRemoves(t *testing.T) {
	prev := []byte(`{"a":"1","b":"2","c":"3"}`)
	fresh := []byte(`{"a":"1","b":"NEW","d":"4"}`)
	got, err := BuildUpdate(prev, fresh)
	if err != nil {
		t.Fatalf("BuildUpdate: %v", err)
	}
	var diff struct {
		Added   map[string]any `json:"added"`
		Changed map[string]any `json:"changed"`
		Removed map[string]any `json:"removed"`
	}
	if err := json.Unmarshal(got, &diff); err != nil {
		t.Fatalf("decode diff: %v\n%s", err, got)
	}
	if diff.Added["d"] != "4" {
		t.Fatalf("added.d = %v", diff.Added["d"])
	}
	if diff.Removed["c"] != "3" {
		t.Fatalf("removed.c = %v", diff.Removed["c"])
	}
	pair, ok := diff.Changed["b"].([]any)
	if !ok || len(pair) != 2 || pair[0] != "2" || pair[1] != "NEW" {
		t.Fatalf("changed.b = %v", diff.Changed["b"])
	}
}

func TestBuildUpdate_NoDifferences(t *testing.T) {
	a := []byte(`{"a":"1"}`)
	got, err := BuildUpdate(a, a)
	if err != nil {
		t.Fatalf("BuildUpdate: %v", err)
	}
	if !strings.Contains(string(got), `"added":{}`) ||
		!strings.Contains(string(got), `"changed":{}`) ||
		!strings.Contains(string(got), `"removed":{}`) {
		t.Fatalf("expected three empty objects: %s", got)
	}
}

func TestBuildUpdate_RejectsInvalidJSON(t *testing.T) {
	if _, err := BuildUpdate([]byte(`{nope`), []byte(`{}`)); err == nil {
		t.Fatal("err nil, want decode error")
	}
}
