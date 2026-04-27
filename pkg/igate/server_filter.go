package igate

import (
	"strings"
	"unicode"
)

// Max byte length of any single appended g/ clause. Splitting is
// semantically neutral since APRS-IS OR's filter keywords.
const composedFilterByteBudget = 512

// ComposeServerFilter appends g/ clauses for tactical callsigns to the
// operator's base filter, deduping case-insensitively against any g/
// already in base. Returns "" on empty input — the caller (client.go
// buildLogin) substitutes the no-match sentinel. Negation tokens like
// "-g/X" are opaque: preserved but not mined for dedup. Tacticals are
// assumed pre-validated by the configstore model.
//
// g/ is the empirically-verified keyword for addressee matching on T2.
func ComposeServerFilter(base string, tacticals []string) string {
	covered := mineCoveredFromBase(base)

	seen := make(map[string]struct{}, len(tacticals))
	var needed []string
	for _, t := range tacticals {
		if t == "" {
			continue
		}
		key := strings.ToUpper(t)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if _, already := covered[key]; already {
			continue
		}
		needed = append(needed, key)
	}

	if len(needed) == 0 {
		return base
	}

	clauses := chunkGClauses(needed, composedFilterByteBudget)

	if base == "" {
		return strings.Join(clauses, " ")
	}
	return base + " " + strings.Join(clauses, " ")
}

func mineCoveredFromBase(base string) map[string]struct{} {
	covered := make(map[string]struct{})
	for _, tok := range strings.FieldsFunc(base, isFilterSeparator) {
		if len(tok) < 2 || tok[0] == '-' {
			continue
		}
		if (tok[0] == 'g' || tok[0] == 'G') && tok[1] == '/' {
			for _, arg := range strings.Split(tok[2:], "/") {
				if arg == "" {
					continue
				}
				covered[strings.ToUpper(arg)] = struct{}{}
			}
		}
	}
	return covered
}

func isFilterSeparator(r rune) bool {
	return unicode.IsSpace(r)
}

// A single callsign longer than budget still gets its own clause; the
// configstore validator prevents that in practice.
func chunkGClauses(calls []string, budget int) []string {
	var clauses []string
	var cur strings.Builder
	cur.WriteString("g/")
	curHasArg := false
	curBytes := len("g/")

	flush := func() {
		if curHasArg {
			clauses = append(clauses, cur.String())
		}
		cur.Reset()
		cur.WriteString("g/")
		curHasArg = false
		curBytes = len("g/")
	}

	for _, c := range calls {
		// Appending to an existing clause costs 1 ("/") + len(c);
		// to an empty "g/" clause, just len(c).
		var add int
		if curHasArg {
			add = 1 + len(c)
		} else {
			add = len(c)
		}
		if curHasArg && curBytes+add > budget {
			flush()
			add = len(c)
		}
		if curHasArg {
			cur.WriteByte('/')
		}
		cur.WriteString(c)
		curHasArg = true
		curBytes += add
	}
	flush()
	return clauses
}
