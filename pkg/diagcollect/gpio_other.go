//go:build !linux

package diagcollect

import "github.com/chrissnell/graywolf/pkg/flareschema"

// CollectGPIO on non-Linux returns nothing + a single not_supported
// issue. /dev/gpiochip* is a Linux-only kernel surface.
func CollectGPIO() ([]flareschema.PTTCandidate, []flareschema.CollectorIssue) {
	return nil, []flareschema.CollectorIssue{{
		Kind:    "not_supported",
		Message: "GPIO chip enumeration is linux-only",
	}}
}

// parseSysfsLabel + parseSysfsNGPIO are exposed on every OS so the
// shared test file can call them. They're trivial string-handling
// functions; the build-tag-gated body is the real work.
func parseSysfsLabel(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != '\n' && s[i] != 0 {
			return s[:i+1]
		}
	}
	return ""
}

func parseSysfsNGPIO(s string) int {
	out := 0
	started := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' {
			if started {
				return out
			}
			continue
		}
		if r < '0' || r > '9' {
			return 0
		}
		out = out*10 + int(r-'0')
		started = true
	}
	if started {
		return out
	}
	return 0
}
