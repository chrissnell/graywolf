package aprs

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCorpus walks pkg/aprs/testdata/corpus.txt and verifies every line
// dispatches to the expected packet type without panicking and without
// returning an error.
func TestCorpus(t *testing.T) {
	path := filepath.Join("testdata", "corpus.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<14), 1<<16)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		if len(raw) == 0 || raw[0] == '#' {
			continue
		}
		// Split on the first '|'; everything after is the info field
		// bytes (preserved verbatim, including spaces).
		sep := bytes.IndexByte(raw, '|')
		if sep < 0 {
			t.Errorf("line %d: no separator", line)
			continue
		}
		wantType := PacketType(string(raw[:sep]))
		info := make([]byte, len(raw)-sep-1)
		copy(info, raw[sep+1:])
		pkt, err := ParseInfo(info)
		if err != nil {
			t.Errorf("line %d (%s): parse: %v", line, wantType, err)
			continue
		}
		if wantType == "thirdparty" {
			wantType = PacketThirdParty
		}
		if pkt.Type != wantType {
			t.Errorf("line %d: got type %q, want %q (info=%q)",
				line, pkt.Type, wantType, strings.TrimSpace(string(info)))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
}
