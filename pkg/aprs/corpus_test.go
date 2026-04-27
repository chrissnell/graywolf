package aprs

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParseInfo exercises ParseInfo with arbitrary byte sequences and
// asserts only that it does not panic. ParseInfo is documented as
// "total" (never panics on malformed input), so any panic surfaced
// here is a real bug in one of the sub-parsers it dispatches to.
//
// The seed corpus is every line from testdata/corpus.txt — the same
// data the TestCorpus table-driven test walks — so the mutator starts
// from real-world representatives of every packet type.
//
// Run with:
//
//	go test -run=^$ -fuzz=FuzzParseInfo ./pkg/aprs/
func FuzzParseInfo(f *testing.F) {
	path := filepath.Join("testdata", "corpus.txt")
	file, err := os.Open(path)
	if err != nil {
		f.Fatalf("open corpus: %v", err)
	}
	defer file.Close()

	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 1<<14), 1<<16)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 || raw[0] == '#' {
			continue
		}
		sep := bytes.IndexByte(raw, '|')
		if sep < 0 {
			continue
		}
		info := make([]byte, len(raw)-sep-1)
		copy(info, raw[sep+1:])
		f.Add(info)
	}
	if err := sc.Err(); err != nil {
		f.Fatalf("scan corpus: %v", err)
	}

	f.Fuzz(func(t *testing.T, info []byte) {
		// Discard the return values — the only assertion is "did not
		// panic". ParseInfo's documented contract is that it never
		// panics on arbitrary input; an error is a valid outcome.
		_, _ = ParseInfo(info)
	})
}

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
