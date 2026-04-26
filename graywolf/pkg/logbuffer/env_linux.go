//go:build linux

package logbuffer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
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
//
// Implementation: statfs gives us the filesystem id, but mapping that
// back to a /dev path requires walking /proc/self/mountinfo. The
// simpler trick — and the one the rest of graywolf doesn't need to
// share — is to read /proc/self/mountinfo and find the longest mount
// point that's a prefix of p.
func backingDeviceForPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	mi, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", fmt.Errorf("read mountinfo: %w", err)
	}
	// Each line: id parent major:minor root mountpoint opts ... - fstype source ...
	// We want fields[4]=mountpoint and fields[9]=source after the "-" separator.
	var bestMount, bestSource string
	for _, line := range strings.Split(string(mi), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		var sepIdx = -1
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
		if !strings.HasPrefix(abs, mount) {
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

// statfsType returns the fs magic for path (used in tests to confirm a
// path lives on tmpfs — magic 0x01021994). Pure helper; no policy.
func statfsType(path string) (int64, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return 0, err
	}
	return int64(s.Type), nil
}
