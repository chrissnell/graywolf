package diagcollect

import (
	"encoding/json"
	"fmt"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// Runner is the seam between collectors and the actual exec call.
// Production code uses defaultRunner, which delegates to RunListing;
// tests inject a fake.
type Runner interface {
	Run(bin, flag string) ([]byte, *flareschema.CollectorIssue)
}

type defaultRunner struct{}

func (defaultRunner) Run(bin, flag string) ([]byte, *flareschema.CollectorIssue) {
	return RunListing(bin, flag)
}

// CollectAudio is the production entry point: invoke --list-audio,
// parse the JSON.
func CollectAudio(bin string) flareschema.AudioDevices {
	return collectAudioWith(defaultRunner{}, bin)
}

func collectAudioWith(r Runner, bin string) flareschema.AudioDevices {
	out := flareschema.AudioDevices{
		Hosts: make([]flareschema.AudioHost, 0),
	}
	raw, issue := r.Run(bin, "--list-audio")
	if issue != nil {
		out.Issues = append(out.Issues, *issue)
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind:    "audio_decode_failed",
			Message: fmt.Sprintf("%v: stdout=%q", err, truncateForIssue(string(raw))),
		})
	}
	return out
}

// truncateForIssue keeps the first ~256 chars of a noisy stdout for
// the issue body so the operator UI doesn't render multi-KB blobs in
// the issues table.
func truncateForIssue(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
