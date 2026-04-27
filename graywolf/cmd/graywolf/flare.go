// Package main contains the graywolf subcommand entry point for
// `graywolf flare`. The bulk of the behaviour lives in
// pkg/diagcollect; this file is the wiring layer responsible for
// flag parsing, exit-code mapping, and stdin/stdout plumbing.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/diagcollect"
	"github.com/chrissnell/graywolf/pkg/diagcollect/redact"
	"github.com/chrissnell/graywolf/pkg/diagcollect/review"
	"github.com/chrissnell/graywolf/pkg/diagcollect/submit"
	"github.com/chrissnell/graywolf/pkg/flareschema"
)

const defaultFlareServer = "https://flare.nw5w.com"

// runFlare is the dispatch entry from main.go. args is os.Args[2:].
// version / gitCommit are the linker-injected build identity.
func runFlare(args []string, version, gitCommit string) int {
	fs := flag.NewFlagSet("graywolf flare", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		dbPath     = fs.String("db", "", "path to graywolf.db (overrides discovery)")
		serverURL  = fs.String("server", defaultFlareServer, "flare-server base URL")
		email      = fs.String("email", "", "user email for portal/email replies (optional)")
		notes      = fs.String("notes", "", "free-text notes (optional)")
		radio      = fs.String("radio", "", "radio model (optional)")
		audio      = fs.String("audio", "", "audio interface model (optional)")
		dryRun     = fs.Bool("dry-run", false, "collect+scrub+review then print payload to stdout (no submit)")
		noLogs     = fs.Bool("no-logs", false, "skip log section")
		noModem    = fs.Bool("no-modem", false, "skip audio/USB/CM108 sections (don't shell out to graywolf-modem)")
		outPath    = fs.String("out", "", "write the prepared payload to FILE instead of submitting")
		resubmitID = fs.String("resubmit", "", "re-collect and update an existing flare by id (requires saved token)")
		modemBin   = fs.String("modem", "", "path to graywolf-modem (overrides discovery)")
		verbose    = fs.Bool("verbose", false, "verbose collection progress to stderr")
		logLimit   = fs.Int("log-limit", 0, "max log rows to include (0 = ring default)")
	)
	// Mirror --verbose to a -v shortcut.
	fs.BoolVar(verbose, "v", false, "alias for --verbose")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 4
	}

	// Discover graywolf.db.
	dbResolved, src, err := diagcollect.DiscoverConfigDB(diagcollect.DiscoverOptions{
		Explicit: *dbPath,
		Env:      os.Getenv("GRAYWOLF_DB"),
	})
	var store *configstore.Store
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v; flare will be degraded\n", err)
	} else {
		if *verbose {
			fmt.Fprintf(os.Stderr, "graywolf.db: %s (%s)\n", dbResolved, src)
		}
		s, oerr := configstore.Open(dbResolved)
		if oerr != nil {
			fmt.Fprintf(os.Stderr, "warning: open %s: %v; flare will be degraded\n", dbResolved, oerr)
		} else {
			store = s
			defer store.Close()
		}
	}

	// Resolve modem path.
	modemResolved, mErr := diagcollect.ResolveModemPath(*modemBin)
	if mErr != nil {
		modemResolved = ""
	}
	modemVer := ""
	modemCommit := ""
	if modemResolved != "" {
		modemVer, modemCommit, _ = readModemVersion(modemResolved)
	}

	// Collect.
	flare, err := diagcollect.Collect(diagcollect.Options{
		ConfigStore:     store,
		ConfigDBPath:    dbResolved,
		ModemBinaryPath: modemResolved,
		User: flareschema.User{
			Email:          *email,
			Notes:          *notes,
			RadioModel:     *radio,
			AudioInterface: *audio,
		},
		Version:      version,
		GitCommit:    gitCommit,
		ModemVersion: modemVer,
		ModemCommit:  modemCommit,
		NoLogs:       *noLogs,
		NoModem:      *noModem,
		LogLimit:     *logLimit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "collect: %v\n", err)
		return 4
	}

	// --out path: dump and exit before review.
	if *outPath != "" {
		body, mErr := json.MarshalIndent(flare, "", "  ")
		if mErr != nil {
			fmt.Fprintf(os.Stderr, "marshal: %v\n", mErr)
			return 4
		}
		if err := os.WriteFile(*outPath, body, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", *outPath, err)
			return 4
		}
		fmt.Printf("wrote %s (%d bytes)\n", *outPath, len(body))
		return 0
	}

	// Review loop.
	eng := redact.NewEngine()
	if hostname, _ := os.Hostname(); hostname != "" {
		eng.SetHostname(hostname)
	}
	outcome, rerr := review.Run(os.Stdin, os.Stdout, flare, eng)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "review: %v\n", rerr)
		return 4
	}
	switch outcome {
	case review.OutcomeCancel:
		fmt.Println("cancelled.")
		return 1
	case review.OutcomeAddNotes:
		fmt.Print("new notes (single line): ")
		updated, _ := readLine(os.Stdin)
		flare.User.Notes = updated
		redact.ScrubFlare(flare, eng)
		nextOutcome, _ := review.Run(os.Stdin, os.Stdout, flare, eng)
		if nextOutcome != review.OutcomeSubmit {
			return 1
		}
	case review.OutcomeSubmit:
		// fallthrough to submission below
	}

	body, mErr := json.Marshal(flare)
	if mErr != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", mErr)
		return 4
	}

	if *dryRun {
		fmt.Println(string(body))
		return 0
	}

	client := submit.NewHTTPClient(*serverURL, nil)
	if *resubmitID != "" {
		return runResubmit(client, *resubmitID, body)
	}
	return runFreshSubmit(client, body)
}

func runFreshSubmit(c submit.Client, body []byte) int {
	resp, err := c.Submit(body)
	return handleSubmitResult(resp, err, body)
}

func runResubmit(c submit.Client, flareID string, body []byte) int {
	dir := submit.StorageDir()
	prev, err := submit.LoadTokenAt(dir, flareID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no saved token for flare %q; submit a fresh flare instead\n", flareID)
		return 1
	}
	resp, err := c.Update(prev.FlareID, body)
	if err == nil {
		_, _ = submit.SaveTokenAt(dir, resp)
	}
	return handleSubmitResult(resp, err, body)
}

func handleSubmitResult(resp flareschema.SubmitResponse, err error, body []byte) int {
	if err == nil {
		fmt.Printf("flare submitted: %s\n", resp.PortalURL)
		_, _ = submit.SaveTokenAt(submit.StorageDir(), resp)
		return 0
	}
	var schemaErr submit.ErrSchemaRejected
	if errors.As(err, &schemaErr) {
		fmt.Fprintf(os.Stderr, "submit rejected (400): %s\n", schemaErr.Body)
		return 1
	}
	var rl submit.ErrRateLimited
	if errors.As(err, &rl) {
		if rl.RetryAfter != "" {
			fmt.Fprintf(os.Stderr, "rate-limited; retry after %s\n", rl.RetryAfter)
		} else {
			fmt.Fprintln(os.Stderr, "rate-limited; retry later")
		}
		return 2
	}
	var srv submit.ErrServerError
	if errors.As(err, &srv) {
		path, sErr := submit.SavePendingFlareAt(submit.PendingDir(), body)
		if sErr != nil {
			fmt.Fprintf(os.Stderr, "server error %d AND failed to save pending: %v\n", srv.Status, sErr)
			return 4
		}
		fmt.Fprintf(os.Stderr, "server error %d; saved pending flare to %s — retry later\n", srv.Status, path)
		return 3
	}
	var tooLarge submit.ErrPayloadTooLarge
	if errors.As(err, &tooLarge) {
		fmt.Fprintln(os.Stderr, tooLarge.Error())
		return 4
	}
	fmt.Fprintf(os.Stderr, "submit failed: %v\n", err)
	return 4
}

// readLine reads one line from in (used for "new notes" re-prompt).
func readLine(in io.Reader) (string, error) {
	buf := make([]byte, 0, 256)
	one := make([]byte, 1)
	for {
		_, err := in.Read(one)
		if err != nil || one[0] == '\n' {
			return string(buf), err
		}
		buf = append(buf, one[0])
	}
}

// readModemVersion shells out to `<bin> --version` and parses the
// "v<version>-<commit>" string the modem emits at startup.
func readModemVersion(bin string) (version, commit string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, rerr := exec.CommandContext(ctx, bin, "--version").Output()
	if rerr != nil {
		return "", "", rerr
	}
	// Format: "graywolf-modem v0.43.2-abcdef1\n"
	for _, tok := range bytes.Fields(out) {
		if len(tok) > 1 && tok[0] == 'v' {
			rest := string(tok[1:])
			if dash := bytes.IndexByte(tok[1:], '-'); dash > 0 {
				return rest[:dash], rest[dash+1:], nil
			}
			return rest, "", nil
		}
	}
	return "", "", nil
}
