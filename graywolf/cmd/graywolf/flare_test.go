package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/diagcollect/redact"
	"github.com/chrissnell/graywolf/pkg/diagcollect/review"
	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// runReviewLoop must consume OutcomeAddNotes internally and only return
// once the user reaches a terminal outcome (s/c/d). The previous
// implementation re-called review.Run exactly once after a notes edit
// and bounced any non-submit second outcome to a generic exit-1.

func TestRunReviewLoop_SubmitOnFirstPrompt(t *testing.T) {
	in := strings.NewReader("s\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := runReviewLoop(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != review.OutcomeSubmit {
		t.Fatalf("outcome = %v, want OutcomeSubmit", got)
	}
}

func TestRunReviewLoop_CancelOnFirstPrompt(t *testing.T) {
	in := strings.NewReader("c\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := runReviewLoop(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != review.OutcomeCancel {
		t.Fatalf("outcome = %v, want OutcomeCancel", got)
	}
}

func TestRunReviewLoop_EditNotesThenSubmit(t *testing.T) {
	in := strings.NewReader("e\nupdated note\ns\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := runReviewLoop(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != review.OutcomeSubmit {
		t.Fatalf("outcome = %v, want OutcomeSubmit", got)
	}
	if f.User.Notes != "updated note" {
		t.Fatalf("notes = %q, want %q", f.User.Notes, "updated note")
	}
}

// Chained edits are the meat of the bug fix: previously the second
// outcome had to be `s` or the user got bounced. Now the loop should
// honour an arbitrary number of edit cycles.
func TestRunReviewLoop_EditTwiceThenSubmit(t *testing.T) {
	in := strings.NewReader("e\nfirst\ne\nsecond\ns\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := runReviewLoop(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != review.OutcomeSubmit {
		t.Fatalf("outcome = %v, want OutcomeSubmit", got)
	}
	if f.User.Notes != "second" {
		t.Fatalf("notes = %q, want %q", f.User.Notes, "second")
	}
}

// Pressing 'c' after editing notes should cancel cleanly — previously
// it returned a generic exit-1 from the linear path. The terminal
// outcome must now propagate so the caller can surface "cancelled."
func TestRunReviewLoop_EditNotesThenCancel(t *testing.T) {
	in := strings.NewReader("e\nrevised\nc\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := runReviewLoop(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != review.OutcomeCancel {
		t.Fatalf("outcome = %v, want OutcomeCancel", got)
	}
}

// Pressing 'd' after editing notes should still propagate as
// OutcomeDiff so the caller can decide whether the current mode
// (--resubmit or fresh) is appropriate.
func TestRunReviewLoop_EditNotesThenDiff(t *testing.T) {
	in := strings.NewReader("e\nrevised\nd\n")
	var out bytes.Buffer
	f := flareschema.BuildSampleFlare()
	got, err := runReviewLoop(in, &out, &f, redact.NewEngine())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != review.OutcomeDiff {
		t.Fatalf("outcome = %v, want OutcomeDiff", got)
	}
}
