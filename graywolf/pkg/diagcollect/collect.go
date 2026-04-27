package diagcollect

import (
	"context"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/diagcollect/redact"
	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// Options drives Collect. ConfigStore + ConfigDBPath + ModemBinaryPath
// are wired by the cmd/graywolf entry point; tests may zero them.
type Options struct {
	// ConfigStore is the opened configstore. Nil produces a degraded
	// flare (config_db_unavailable issue).
	ConfigStore *configstore.Store
	// ConfigDBPath is the resolved graywolf.db path. Used to find
	// graywolf-logs.db (next to it) when LogLimit > 0.
	ConfigDBPath string
	// ModemBinaryPath is the resolved graywolf-modem path. Empty
	// produces a modem_unavailable issue in audio/usb/cm108.
	ModemBinaryPath string

	// Runner is the collector exec stub. nil -> defaultRunner{}.
	Runner Runner

	// User holds the operator-supplied flag values (--email, --notes,
	// --radio, --audio).
	User flareschema.User

	// Version, GitCommit, ModemVersion populate Meta.
	Version      string
	GitCommit    string
	ModemVersion string
	ModemCommit  string

	// NoLogs skips the logs collector. NoModem skips
	// audio/usb/cm108 (each one records a skipped_no_modem issue
	// so the operator UI can tell "skipped" from "missing").
	NoLogs  bool
	NoModem bool

	// LogLimit caps the rows pulled from graywolf-logs.db. 0 means
	// "use the default" (5000, matching Plan 1's disk-backed ring).
	LogLimit int
}

// Collect runs every collector and returns a fully scrubbed Flare
// ready for review or submission.
func Collect(opts Options) (*flareschema.Flare, error) {
	runner := opts.Runner
	if runner == nil {
		runner = defaultRunner{}
	}

	flare := &flareschema.Flare{
		SchemaVersion: flareschema.SchemaVersion,
		User:          opts.User,
	}

	// System (also yields literal hostname for the redact engine).
	sysRes := CollectSystem()
	flare.System = sysRes.System

	// Service status.
	flare.ServiceStatus = CollectServiceStatus()

	// Config dump.
	if opts.ConfigStore != nil {
		flare.Config = CollectConfig(context.Background(), opts.ConfigStore)
	} else {
		flare.Config = flareschema.ConfigSection{
			Issues: []flareschema.CollectorIssue{{
				Kind:    "config_db_unavailable",
				Message: "configstore not opened by caller",
			}},
		}
	}

	// PTT serial + GPIO appended.
	flare.PTT = CollectPTT()
	if gpioCands, gpioIssues := CollectGPIO(); len(gpioCands) > 0 || len(gpioIssues) > 0 {
		flare.PTT.Candidates = append(flare.PTT.Candidates, gpioCands...)
		flare.PTT.Issues = append(flare.PTT.Issues, gpioIssues...)
	}

	// GPS.
	flare.GPS = CollectGPS()

	// Modem-driven sections.
	if opts.NoModem {
		skip := flareschema.CollectorIssue{Kind: "skipped_no_modem", Message: "--no-modem"}
		flare.AudioDevices.Issues = []flareschema.CollectorIssue{skip}
		flare.USBTopology.Issues = []flareschema.CollectorIssue{skip}
		flare.CM108.Issues = []flareschema.CollectorIssue{skip}
	} else {
		flare.AudioDevices = collectAudioWith(runner, opts.ModemBinaryPath)
		flare.USBTopology = collectUSBWith(runner, opts.ModemBinaryPath)
		flare.CM108 = collectCM108With(runner, opts.ModemBinaryPath)
	}

	// Logs.
	if opts.NoLogs {
		flare.Logs = flareschema.LogsSection{
			Source:  "graywolf-logs.db",
			Entries: []flareschema.LogEntry{},
			Issues: []flareschema.CollectorIssue{{
				Kind: "skipped_no_logs", Message: "--no-logs",
			}},
		}
	} else {
		flare.Logs = CollectLogs(opts.ConfigDBPath, opts.LogLimit)
	}

	// Meta.
	flare.Meta = flareschema.Meta{
		SchemaVersion:        flareschema.SchemaVersion,
		GraywolfVersion:      opts.Version,
		GraywolfCommit:       opts.GitCommit,
		GraywolfModemVersion: opts.ModemVersion,
		GraywolfModemCommit:  opts.ModemCommit,
		HostnameHash:         redact.HashHostname(sysRes.Hostname),
		SubmittedAt:          time.Now().UTC(),
	}

	// Redaction.
	eng := redact.NewEngine()
	eng.SetHostname(sysRes.Hostname)
	redact.ScrubFlare(flare, eng)

	return flare, nil
}

// CollectWithEngine re-runs only the scrub pass against an updated engine
// without re-walking the OS. Intended for the review TUI's ad-hoc redaction.
func CollectWithEngine(opts Options, eng *redact.Engine) (*flareschema.Flare, error) {
	flare, err := Collect(opts)
	if err != nil {
		return nil, err
	}
	redact.ScrubFlare(flare, eng)
	return flare, nil
}
