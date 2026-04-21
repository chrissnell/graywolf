package releasenotes

import (
	"strconv"
	"strings"
)

// Compare returns -1, 0, or 1 based on numeric x.y.z comparison.
//
// An empty string sorts strictly before any real version: Compare("",
// "0.0.0") == -1 and Compare("", "") == 0. This matches the fresh-user
// semantics where a WebUser with no LastSeenReleaseVersion sees every
// note.
//
// Beta suffixes. The Makefile injects main.Version from VERSION only,
// which is bare x.y.z even on beta builds (see Makefile bump-beta:
// `echo "$(NEW)" > VERSION` is bare; the -beta.N suffix only lives on
// the git tag, not in main.Version). So under normal flow Compare never
// sees a beta suffix. However, dev / unlabelled builds can still have
// trailing non-numeric garbage if someone cross-wires the ldflags, and
// an operator could theoretically seed LastSeenReleaseVersion from an
// older code path. We defensively truncate at the first character that
// is not a digit or dot, before numeric parsing; this collapses
// "0.11.0-beta.3", "0.11.0-rc1", and "0.11.0-abc1234-dirty" to
// "0.11.0".
//
// A beta tester therefore sees whatever release note is keyed to the
// bare MAJOR.MINOR.PATCH they're pre-releasing; that's acceptable,
// since betas are opt-in and note content is identical.
func Compare(a, b string) int {
	a = strip(a)
	b = strip(b)
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	as := splitParts(a)
	bs := splitParts(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(as) {
			ai = as[i]
		}
		if i < len(bs) {
			bi = bs[i]
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return 0
}

// strip truncates at the first non-digit, non-dot character. Leaves
// bare x.y.z untouched; reduces "0.11.0-beta.3" to "0.11.0" and
// "0.11.0-abc1234-dirty" to "0.11.0".
func strip(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9') && c != '.' {
			return s[:i]
		}
	}
	return s
}

// splitParts parses a dotted numeric string into ints. Malformed
// components become 0 rather than returning an error — comparison is
// best-effort and should never panic on a weird stored value.
func splitParts(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
