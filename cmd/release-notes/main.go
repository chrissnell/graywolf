// Command release-notes renders the operator-facing changelog for a given
// release version from the embedded release notes (pkg/releasenotes/notes.yaml)
// and writes it to a file, for use as the GitHub release body.
//
// The Release workflow runs this on a tag push and points GoReleaser's
// --release-notes flag at the output, so the GitHub release description is
// the same curated note that drives the in-app "What's new" popup -- not
// GoReleaser's git-derived changelog, which lists every commit when the
// previous release tag is not an ancestor of the tagged commit.
//
//	go run ./cmd/release-notes -version 0.14.0 -out release-notes.md
//
// Unlike play-whatsnew, the output is NOT truncated: the GitHub release
// body has no tight length limit, so the full note ships. Exits non-zero
// if no note exists for the version, so a release with a missing changelog
// entry fails loudly rather than shipping a blank body.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chrissnell/graywolf/pkg/releasenotes"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses args, renders the note, and writes it, returning the process
// exit code. A non-zero return is what the Release workflow relies on to
// fail loudly when notes.yaml has no entry for the tag version, rather than
// publishing a blank release body.
func run(args []string) int {
	fs := flag.NewFlagSet("release-notes", flag.ContinueOnError)
	version := fs.String("version", "", "release version (x.y.z, no leading v) to render notes for")
	out := fs.String("out", "", "output file path (e.g. release-notes.md)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *version == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: release-notes -version <x.y.z> -out <file>")
		return 2
	}

	text, err := releasenotes.PlainText(*version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "release-notes: %v\n", err)
		return 1
	}

	if dir := filepath.Dir(*out); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "release-notes: create %s: %v\n", dir, err)
			return 1
		}
	}
	if err := os.WriteFile(*out, []byte(text+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "release-notes: write %s: %v\n", *out, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "release-notes: wrote %d chars to %s\n", len([]rune(text)), *out)
	return 0
}
