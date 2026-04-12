package aprs

import (
	"sort"
	"strings"
)

// DeviceInfo identifies an APRS device from its tocall or mic-e identifier.
type DeviceInfo struct {
	Vendor string `json:"vendor,omitempty"`
	Model  string `json:"model,omitempty"`
	Class  string `json:"class,omitempty"`
}

type tocallEntry struct {
	pattern string
	info    DeviceInfo
}

type miceLegacyEntry struct {
	prefix string
	suffix string
	info   DeviceInfo
}

func init() {
	// Sort tocalls: longer/more-specific patterns first so first match wins.
	sort.Slice(tocallDB, func(i, j int) bool {
		si := specificity(tocallDB[i].pattern)
		sj := specificity(tocallDB[j].pattern)
		if si != sj {
			return si > sj
		}
		return len(tocallDB[i].pattern) > len(tocallDB[j].pattern)
	})
}

// specificity returns how many literal (non-wildcard) characters a pattern has.
func specificity(p string) int {
	n := 0
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '?', '*':
		default:
			if p[i] >= 'a' && p[i] <= 'z' {
				// lowercase = digit wildcard (e.g. 'n')
				continue
			}
			n++
		}
	}
	return n
}

// LookupTocall returns device info for an APRS destination callsign.
// Returns nil if no match found.
func LookupTocall(dest string) *DeviceInfo {
	// Strip SSID if present
	if idx := strings.IndexByte(dest, '-'); idx >= 0 {
		dest = dest[:idx]
	}
	dest = strings.TrimRight(dest, " ")
	if len(dest) == 0 {
		return nil
	}

	for i := range tocallDB {
		if matchPattern(dest, tocallDB[i].pattern) {
			info := tocallDB[i].info
			return &info
		}
	}
	return nil
}

// matchPattern matches a destination callsign against a tocall pattern.
// Pattern characters: ? = any single char, * = any remaining chars,
// lowercase a-z = any single digit (0-9).
func matchPattern(dest, pattern string) bool {
	if len(pattern) == 0 {
		return false
	}
	// Trailing * = prefix match
	if pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		if len(dest) < len(prefix) {
			return false
		}
		return matchChars(dest[:len(prefix)], prefix)
	}
	if len(dest) != len(pattern) {
		return false
	}
	return matchChars(dest, pattern)
}

func matchChars(s, pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		p := pattern[i]
		switch {
		case p == '?':
			// matches any character
		case p >= 'a' && p <= 'z':
			// digit wildcard
			if s[i] < '0' || s[i] > '9' {
				return false
			}
		default:
			if s[i] != p {
				return false
			}
		}
	}
	return true
}

// LookupMicEDevice returns device info from a Mic-E comment/status string.
// It checks both the modern 2-char suffix table and the legacy prefix/suffix table.
func LookupMicEDevice(comment string) *DeviceInfo {
	if len(comment) < 2 {
		return nil
	}

	// Modern Mic-E: last 2 chars of comment
	suffix := comment[len(comment)-2:]
	if info, ok := miceDB[suffix]; ok {
		return &info
	}

	// Legacy Mic-E: first char prefix, optional last char suffix
	prefix := comment[:1]
	var legacySuffix string
	if len(comment) > 1 {
		legacySuffix = comment[len(comment)-1:]
	}

	// Try prefix+suffix match first (more specific), then prefix-only
	var prefixOnly *DeviceInfo
	for i := range miceLegacyDB {
		e := &miceLegacyDB[i]
		if e.prefix != prefix {
			continue
		}
		if e.suffix != "" && e.suffix == legacySuffix {
			info := e.info
			return &info
		}
		if e.suffix == "" && prefixOnly == nil {
			info := e.info
			prefixOnly = &info
		}
	}
	return prefixOnly
}
