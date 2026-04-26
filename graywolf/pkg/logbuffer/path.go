package logbuffer

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveOptions are the inputs to the path picker. All fields are
// either CLI-flag-derived or detector outputs so the picker itself stays
// pure and testable.
type ResolveOptions struct {
	// ConfigDBPath is the path the operator gave for graywolf.db. Used as
	// the disk-backed fallback location (logs land in the same directory
	// under graywolf-logs.db).
	ConfigDBPath string

	// PreferRamdisk forces ramdisk preference even on non-SD systems.
	// Wired from --logbuffer-ramdisk.
	PreferRamdisk bool

	// IsRaspberryPi mirrors isRaspberryPi() — kept as a separate field so
	// callers can override for tests and so the structured form documents
	// the inputs explicitly.
	IsRaspberryPi bool

	// BackingIsSDCard is true when the directory holding the config DB is
	// backed by an mmcblk/mtdblock device.
	BackingIsSDCard bool

	// WritableProbe is invoked for each candidate ramdisk directory. The
	// first directory the probe accepts wins. In production this is
	// defaultWritableProbe (creates and removes a temp file).
	WritableProbe func(dir string) error
}

// ramdiskCandidates lists the tmpfs locations we try in order. /run is
// preferred because the operator can mount a per-service tmpfs there
// (RuntimeDirectory= in the systemd unit); /dev/shm is the fallback
// because it's tmpfs on every Linux kernel by default.
var ramdiskCandidates = []string{
	"/run/graywolf",
	"/dev/shm/graywolf",
}

// ResolvePath returns the absolute path where graywolf-logs.db should
// live. The returned directory is NOT guaranteed to exist — Open()
// creates it.
func ResolvePath(opts ResolveOptions) (string, error) {
	useRamdisk := opts.PreferRamdisk || opts.BackingIsSDCard || opts.IsRaspberryPi
	if useRamdisk {
		probe := opts.WritableProbe
		if probe == nil {
			probe = defaultWritableProbe
		}
		for _, dir := range ramdiskCandidates {
			if err := probe(dir); err == nil {
				return filepath.Join(dir, "graywolf-logs.db"), nil
			}
		}
		// Fall through to disk default.
	}
	defaultDir := filepath.Dir(opts.ConfigDBPath)
	return filepath.Join(defaultDir, "graywolf-logs.db"), nil
}

// defaultWritableProbe attempts to create the directory then write and
// remove a small temp file inside it. Returns the first error
// encountered. Used in production by ResolvePath.
func defaultWritableProbe(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".logbuffer-probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// IsRamdiskPath reports whether p was placed under one of the tmpfs
// candidates ResolvePath tries (i.e. /run/graywolf or /dev/shm/graywolf).
// Used by cmd/graywolf to detect a "wanted ramdisk but fell back to disk"
// outcome and surface the spec-required WARN. Kept here so the candidate
// list stays the single source of truth.
func IsRamdiskPath(p string) bool {
	for _, dir := range ramdiskCandidates {
		if strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}
