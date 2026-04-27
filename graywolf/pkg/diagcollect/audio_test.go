package diagcollect

import (
	"errors"
	"testing"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// fakeRunner is the test double for the Runner interface, reused by
// audio_test.go, usb_test.go, and cm108_test.go.
type fakeRunner struct {
	out   []byte
	issue *flareschema.CollectorIssue
}

func (r fakeRunner) Run(bin, flag string) ([]byte, *flareschema.CollectorIssue) {
	return r.out, r.issue
}

func TestCollectAudio_HappyPath(t *testing.T) {
	canned := []byte(`{"hosts":[{"id":"alsa","name":"ALSA","is_default":true,"devices":[]}]}`)
	got := collectAudioWith(fakeRunner{out: canned}, "/fake/modem")
	if len(got.Hosts) != 1 || got.Hosts[0].ID != "alsa" {
		t.Fatalf("unexpected: %+v", got)
	}
	if len(got.Issues) != 0 {
		t.Fatalf("issues = %+v, want empty", got.Issues)
	}
}

func TestCollectAudio_RunnerIssuePropagates(t *testing.T) {
	got := collectAudioWith(fakeRunner{
		issue: &flareschema.CollectorIssue{Kind: "modem_unavailable", Message: "missing"},
	}, "")
	if len(got.Hosts) != 0 {
		t.Fatalf("Hosts not empty: %+v", got.Hosts)
	}
	if len(got.Issues) != 1 || got.Issues[0].Kind != "modem_unavailable" {
		t.Fatalf("issues = %+v", got.Issues)
	}
}

func TestCollectAudio_MalformedJSONBecomesIssue(t *testing.T) {
	got := collectAudioWith(fakeRunner{out: []byte(`{not valid`)}, "/fake/modem")
	if len(got.Issues) != 1 || got.Issues[0].Kind != "audio_decode_failed" {
		t.Fatalf("issues = %+v, want audio_decode_failed", got.Issues)
	}
}

func TestRunnerInterface_DefaultUsesRunListing(t *testing.T) {
	// Compile-time check that defaultRunner satisfies Runner.
	var _ Runner = defaultRunner{}
	_ = errors.New
}
