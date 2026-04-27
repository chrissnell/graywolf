package review

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chrissnell/graywolf/pkg/diagcollect/redact"
	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// Outcome reports the user's decision. The caller dispatches on this
// to actually submit, cancel, re-prompt for notes, etc. The review
// loop never performs network IO — it just renders + decides.
type Outcome int

const (
	OutcomeSubmit Outcome = iota + 1
	OutcomeCancel
	OutcomeAddNotes
	OutcomeAddRedaction
	OutcomeDiff
)

func (o Outcome) String() string {
	switch o {
	case OutcomeSubmit:
		return "submit"
	case OutcomeCancel:
		return "cancel"
	case OutcomeAddNotes:
		return "add_notes"
	case OutcomeAddRedaction:
		return "add_redaction"
	case OutcomeDiff:
		return "diff"
	}
	return "unknown"
}

const pageLines = 24

// Run paginates the JSON-rendered payload, prompts for one keystroke,
// and returns the resulting Outcome. The payload is mutated in place
// only when the user adds an ad-hoc redaction (via the engine the
// caller passed in).
func Run(in io.Reader, out io.Writer, payload *flareschema.Flare, eng *redact.Engine) (Outcome, error) {
	r := bufio.NewReader(in)
	for {
		render, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return OutcomeCancel, fmt.Errorf("render payload: %w", err)
		}
		paginate(out, render)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "APRS callsigns are NOT redacted; everything else above has been scrubbed.")
		fmt.Fprintln(out, "[s]ubmit  [c]ancel  [e]dit notes  [r]edact regex  [d]iff (resubmit only)  [enter] page")
		fmt.Fprint(out, "> ")
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return OutcomeCancel, nil
		}
		key := strings.TrimSpace(line)
		switch key {
		case "s":
			return OutcomeSubmit, nil
		case "c", "q":
			return OutcomeCancel, nil
		case "e":
			return OutcomeAddNotes, nil
		case "d":
			return OutcomeDiff, nil
		case "r":
			fmt.Fprint(out, "regex to redact: ")
			pat, err := r.ReadString('\n')
			if err != nil {
				continue
			}
			pat = strings.TrimSpace(pat)
			if err := eng.AddRegex(pat, "[REDACTED]"); err != nil {
				fmt.Fprintf(out, "invalid regex: %v\n", err)
				continue
			}
			redact.ScrubFlare(payload, eng)
			fmt.Fprintln(out, "ad-hoc redaction applied; review the updated payload")
		case "":
			// Empty input → advance one page (rendered above).
			continue
		default:
			fmt.Fprintf(out, "unknown key %q; press s/c/e/r/d or enter\n", key)
		}
	}
}

// paginate writes data to out in pages of pageLines lines, separated
// by a "-- continue --" marker. The function does NOT block for
// keypresses between pages — pagination is cosmetic in this simple TUI.
func paginate(out io.Writer, data []byte) {
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		fmt.Fprintln(out, l)
		if (i+1)%pageLines == 0 && i+1 < len(lines) {
			fmt.Fprintln(out, "  -- continue --")
		}
	}
}
