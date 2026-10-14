package igate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// rfToISBlocklist is a local operator policy independent of the APRS-IS
// server's receive filter. Base callsigns match all SSIDs, while entries
// containing an SSID match only that explicit identifier.
type rfToISBlocklist struct {
	base  map[string]struct{}
	exact map[string]struct{}
}

var rfToISCallPattern = regexp.MustCompile(`^[A-Z0-9]{1,6}(?:-([0-9]{1,2}))?$`)

// ParseRFToISBlocklist validates and canonicalizes a comma/whitespace
// separated list of callsigns. Empty is allowed (no source exclusions).
// The API enforces this parser before saving; the runtime tolerates bad
// hand-edited config by treating an invalid list as empty.
func ParseRFToISBlocklist(input string) (string, error) {
	tokens := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	if len(tokens) > 100 {
		return "", fmt.Errorf("rf_to_is_blocklist: maximum 100 entries")
	}
	unique := make(map[string]struct{}, len(tokens))
	ordered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		call := strings.ToUpper(strings.TrimSpace(token))
		parts := rfToISCallPattern.FindStringSubmatch(call)
		if parts == nil {
			return "", fmt.Errorf("rf_to_is_blocklist: invalid callsign %q", token)
		}
		if parts[1] != "" {
			ssid, _ := strconv.Atoi(parts[1])
			if ssid > 15 {
				return "", fmt.Errorf("rf_to_is_blocklist: SSID above 15 in %q", token)
			}
			// Keep explicit -0 distinct from a bare callsign: -0
			// means only SSID 0, while bare K1ABC matches all SSIDs.
			base := strings.SplitN(call, "-", 2)[0]
			call = fmt.Sprintf("%s-%d", base, ssid)
		}
		if _, ok := unique[call]; ok {
			continue
		}
		unique[call] = struct{}{}
		ordered = append(ordered, call)
	}
	return strings.Join(ordered, "\n"), nil
}

func newRFToISBlocklist(value string) *rfToISBlocklist {
	result := &rfToISBlocklist{
		base:  make(map[string]struct{}),
		exact: make(map[string]struct{}),
	}
	canonical, err := ParseRFToISBlocklist(value)
	if err != nil {
		return result // Fail open for invalid hand-edited config; API rejects it.
	}
	for _, call := range strings.Fields(canonical) {
		if strings.Contains(call, "-") {
			result.exact[call] = struct{}{}
		} else {
			result.base[call] = struct{}{}
		}
	}
	return result
}

func (b *rfToISBlocklist) blocks(source string) bool {
	if b == nil {
		return false
	}
	source = strings.ToUpper(strings.TrimSpace(source))
	if _, ok := b.exact[source]; ok {
		return true
	}
	if !strings.Contains(source, "-") {
		if _, ok := b.exact[source+"-0"]; ok {
			return true
		}
	}
	base, _, _ := strings.Cut(source, "-")
	_, ok := b.base[base]
	return ok
}
