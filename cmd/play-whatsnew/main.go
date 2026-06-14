// Command play-whatsnew renders the Google Play "What's new" text for a
// given release version from the embedded release-notes changelog
// (pkg/releasenotes/notes.yaml) and writes it to a file.
//
// The Android release workflow runs this before uploading the AAB to the
// Play Closed Testing track and points the upload's whatsNewDirectory at
// the output, so each tagged release ships an operator-facing changelog
// derived from the same notes that drive the in-app "What's new" popup.
//
//	go run ./cmd/play-whatsnew -version 0.13.16 -out staging/whatsnew/whatsnew-en-US
//
// Output is truncated to Play's 500-character per-language limit at a
// sentence or word boundary. Exits non-zero if no note exists for the
// version, so a release with a missing changelog entry fails loudly.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chrissnell/graywolf/pkg/releasenotes"
)

func main() {
	version := flag.String("version", "", "release version (x.y.z, no leading v) to render notes for")
	out := flag.String("out", "", "output file path (e.g. whatsnew/whatsnew-en-US)")
	max := flag.Int("max", releasenotes.PlayWhatsNewMax, "maximum characters (Play caps the field at 500)")
	flag.Parse()

	if *version == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: play-whatsnew -version <x.y.z> -out <file> [-max N]")
		os.Exit(2)
	}

	text, err := releasenotes.PlainText(*version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "play-whatsnew: %v\n", err)
		os.Exit(1)
	}
	text = releasenotes.Truncate(text, *max)

	if dir := filepath.Dir(*out); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "play-whatsnew: create %s: %v\n", dir, err)
			os.Exit(1)
		}
	}
	// Trailing newline keeps the file POSIX-clean; Play trims surrounding
	// whitespace on ingest.
	if err := os.WriteFile(*out, []byte(text+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "play-whatsnew: write %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "play-whatsnew: wrote %d chars to %s\n", len([]rune(text)), *out)
}
