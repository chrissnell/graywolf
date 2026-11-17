package pttdevice

import (
	"fmt"
	"os"
	"path/filepath"
)

// withSerialByID adds existing udev aliases for known serial devices. It only
// reads directory/symlink metadata: discovery must not open or key a port.
// Keep kernel paths too, including a previously saved selection. Unknown USB
// devices inherit their original recommendation rather than becoming PTT devices.
func withSerialByID(devs []AvailableDevice, dir string) []AvailableDevice {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return devs
	}
	known := make(map[string]AvailableDevice)
	seen := make(map[string]bool)
	for _, d := range devs {
		seen[d.Path] = true
		if d.Type != "serial" {
			continue
		}
		target, err := filepath.EvalSymlinks(d.Path)
		if err == nil {
			known[target] = d
		}
	}
	out := append([]AvailableDevice(nil), devs...)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if seen[path] {
			continue
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		original, ok := known[target]
		if !ok {
			continue
		}
		st, err := os.Stat(path)
		if err != nil || st.Mode()&os.ModeCharDevice == 0 {
			continue
		}
		alias := original
		alias.Path = path
		alias.Name = entry.Name()
		label := original.Description
		if label == "" {
			label = original.Name
		}
		alias.Description = fmt.Sprintf("%s — persistent ID (currently %s)", label, original.Path)
		out = append(out, alias)
		seen[path] = true
	}
	return out
}
