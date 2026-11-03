package tncprofile

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

// A serial read may split a reply anywhere, including within cmd:.
type bytewiseReader struct{ io.Reader }

func (r bytewiseReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

func TestKenwoodResponseRejectsErrorImmediatelyBeforePrompt(t *testing.T) {
	for _, text := range []string{"?EHcmd:", "Ncmd:", "ERRORcmd:", "?EH\rcmd:"} {
		r := bufio.NewReader(bytewiseReader{strings.NewReader(text)})
		_, err := readKenwoodResponse(r, true, func(s string) bool { return strings.HasPrefix(s, "ID ") })
		if err == nil || !strings.Contains(err.Error(), "rejected") {
			t.Errorf("%q: expected explicit rejection, got %v", text, err)
		}
	}
}

func TestKenwoodKISSReplyRequiresKnownSetting(t *testing.T) {
	for _, text := range []string{"KISS was INVALID\rcmd:", "KISS was \rcmd:", "KISS OFF\rcmd:"} {
		if _, err := readKenwoodPrompt(bufio.NewReader(strings.NewReader(text)), true); err == nil {
			t.Errorf("invalid setting accepted: %q", text)
		}
	}
}

func TestKenwoodFragmentedPromptAndStartupBytes(t *testing.T) {
	for _, text := range []string{"cmd:\rKISS was OFF\rcmd:", "KISS was ON\ncmd:", "KISS ON\rcmd:", "KISS was OFFcmd:", "cmd:\rKISS     was OFF\r\ncmd:", "KISS\twas\tON\r\ncmd:"} {
		r := bufio.NewReader(bytewiseReader{strings.NewReader(text + "\xc0\x00\x42\xc0")})
		if _, err := readKenwoodPrompt(r, true); err != nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		frame, err := io.ReadAll(r)
		if err != nil || string(frame) != "\xc0\x00\x42\xc0" {
			t.Errorf("lost buffered KISS bytes: %x, %v", frame, err)
		}
	}
}
