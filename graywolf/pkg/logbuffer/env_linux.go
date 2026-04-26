//go:build linux

package logbuffer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const piModelPath = "/sys/firmware/devicetree/base/model"

// isRaspberryPi returns true when the device-tree model string contains
// the Raspberry Pi marker. Used at startup to bias the default ring
// size and trigger ramdisk preference.
func isRaspberryPi(modelPath string) bool {
	data, err := os.ReadFile(modelPath)
	if err != nil {
		return false
	}
	// The kernel writes a NUL-terminated string; trim it before substring
	// matching so the test fixtures don't have to.
	data = bytes.TrimRight(data, "\x00\n ")
	return bytes.Contains(data, []byte("Raspberry Pi"))
}

// isSDCardDevice reports whether dev looks like an SD-card / eMMC /
// raw-NAND block device. The check is name-based because the alternative
// (tracing minor numbers through subsystems) varies across kernel
// versions.
func isSDCardDevice(dev string) bool {
	base := filepath.Base(dev)
	switch {
	case strings.HasPrefix(base, "mmcblk"):
		return true
	case strings.HasPrefix(base, "mtdblock"):
		return true
	}
	return false
}

// backingDeviceForPath returns the /dev path of the block device backing
// the filesystem at p. Used by ResolvePath to decide whether the data
// dir lives on an SD card.
func backingDeviceForPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	mi, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", fmt.Errorf("read mountinfo: %w", err)
	}
	return parseMountinfo(mi, abs)
}

// parseMountinfo returns the longest mount whose mountpoint is a
// path-component prefix of abs. Pulled out of backingDeviceForPath so
// the prefix logic can be unit-tested against fixture content rather
// than the host's real /proc.
//
// Format reminder: each line is
//   id parent major:minor root mountpoint opts ... - fstype source ...
// fields[4] is the mountpoint; the source field follows the literal "-"
// separator at offset +2. Mount options can themselves contain a "-"
// token only after the separator (kernel never emits one before), so
// scanning left-to-right for the first "-" is safe.
//
// Known limitation: the kernel octal-escapes whitespace and a few other
// characters in mountpoint/source (\040 for space, etc.). graywolf's
// install paths are ASCII-without-whitespace by convention, so we don't
// decode these escapes today; if that ever changes, decode here.
func parseMountinfo(content []byte, abs string) (string, error) {
	var bestMount, bestSource string
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		sepIdx := -1
		for i, f := range fields {
			if f == "-" {
				sepIdx = i
				break
			}
		}
		if sepIdx < 0 || sepIdx+2 >= len(fields) {
			continue
		}
		mount := fields[4]
		source := fields[sepIdx+2]
		// Path-component prefix: mount must equal abs OR be its
		// ancestor. Reject "/var/lib/foo" being treated as an ancestor
		// of "/var/lib/foobar".
		switch {
		case mount == abs:
			// exact match
		case mount == "/":
			// root is an ancestor of everything
		case strings.HasPrefix(abs, mount+"/"):
			// proper ancestor with path-component boundary
		default:
			continue
		}
		if len(mount) > len(bestMount) {
			bestMount = mount
			bestSource = source
		}
	}
	if bestSource == "" {
		return "", fmt.Errorf("no mount found for %q", abs)
	}
	return bestSource, nil
}
