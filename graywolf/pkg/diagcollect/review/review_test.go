package review

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/diagcollect/redact"
	"github.com/chrissnell/graywolf/pkg/flareschema"
)

func TestRun_SubmitOnFirstPrompt(t *testing.T) {
	in := strings.NewReader("s\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := Run(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != OutcomeSubmit {
		t.Fatalf("outcome = %v, want OutcomeSubmit", got)
	}
}

func TestRun_CancelKeystrokes(t *testing.T) {
	for _, key := range []string{"c\n", "q\n"} {
		in := strings.NewReader(key)
		var out bytes.Buffer
		f := flareschema.BuildSampleFlare()
		got, err := Run(in, &out, &f, redact.NewEngine())
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got != OutcomeCancel {
			t.Fatalf("key %q outcome = %v, want OutcomeCancel", key, got)
		}
	}
}

func TestRun_PaginationAdvancesThenSubmits(t *testing.T) {
	in := strings.NewReader("\n\n\ns\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := Run(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != OutcomeSubmit {
		t.Fatalf("outcome = %v, want OutcomeSubmit", got)
	}
	if !strings.Contains(out.String(), "[s]ubmit") {
		t.Fatalf("prompt not rendered:\n%s", out.String())
	}
}

func TestRun_AddRedactionRegex(t *testing.T) {
	in := strings.NewReader("r\nsecret-[0-9]+\ns\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	f.User.Notes = "open secret-1234 in trace"
	eng := redact.NewEngine()
	got, err := Run(in, &out, &f, eng)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != OutcomeSubmit {
		t.Fatalf("outcome = %v, want OutcomeSubmit", got)
	}
	if strings.Contains(f.User.Notes, "secret-1234") {
		t.Fatalf("ad-hoc redact didn't fire: %q", f.User.Notes)
	}
}

func TestRun_AddInvalidRegexShowsErrorAndContinues(t *testing.T) {
	in := strings.NewReader("r\n[unterminated\ns\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := Run(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != OutcomeSubmit {
		t.Fatalf("outcome = %v, want OutcomeSubmit", got)
	}
	if !strings.Contains(out.String(), "regex") {
		t.Fatalf("regex error not surfaced:\n%s", out.String())
	}
}

func TestRun_EditNotesOutcome(t *testing.T) {
	in := strings.NewReader("e\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := Run(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != OutcomeAddNotes {
		t.Fatalf("outcome = %v, want OutcomeAddNotes", got)
	}
}
