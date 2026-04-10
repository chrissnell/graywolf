// Package serial opens serial / bluetooth rfcomm devices for the KISS
// server. It is intentionally a thin wrapper so that the concrete
// serial library dependency (go.bug.st/serial) is isolated from the
// rest of the codebase and can be swapped or removed on platforms
// where a serial KISS interface is not required.
//
// Phase 4 note: the go.bug.st/serial dependency has NOT yet been
// pulled into go.mod because this agent runs in a sandboxed worktree
// without network access. The Open function below is a stub that
// returns an error; a follow-up change will run `go mod tidy` and
// replace the stub body with a real go.bug.st/serial.Open call. The
// public API (Config, Open) will not change, so call sites in
// cmd/graywolf can be wired up now against this package.
package serial

import (
	"errors"
	"io"
)

// Config describes a serial-attached KISS TNC.
type Config struct {
	// Device is the OS device path (e.g. "/dev/ttyUSB0") or a
	// bluetooth rfcomm device path.
	Device string
	// BaudRate in bits per second. Defaults to 9600 if zero.
	BaudRate int
	// DataBits, Parity, StopBits follow go.bug.st/serial conventions;
	// zero values yield 8-N-1.
	DataBits int
	Parity   string // "none"|"odd"|"even"
	StopBits int    // 1 or 2 (half stop bits not supported)
}

// Open opens the configured serial port and returns an
// io.ReadWriteCloser suitable for handoff to kiss.Server.ServeTransport.
//
// NOTE: Phase 4 stub. Returns ErrNotImplemented until go.bug.st/serial
// is added to go.mod.
func Open(cfg Config) (io.ReadWriteCloser, error) {
	_ = cfg
	return nil, ErrNotImplemented
}

// ErrNotImplemented is returned by Open until the real serial backend
// is wired in.
var ErrNotImplemented = errors.New("kiss/serial: go.bug.st/serial backend not yet wired; run `go mod tidy` and replace Open stub")
