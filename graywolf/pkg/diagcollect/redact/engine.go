package redact

import (
	"fmt"
	"regexp"
)

// Engine applies the built-in rules plus any ad-hoc regexes the
// operator added through the review TUI. Each Engine instance carries
// its own ad-hoc rules — no global state.
type Engine struct {
	builtin []Rule
	adhoc   []Rule
}

// NewEngine returns a fresh Engine with the default rule set. The
// hostname rule is wired but inert until SetHostname is called.
func NewEngine() *Engine {
	return &Engine{builtin: BuiltinRules()}
}

// SetHostname populates the hostname rule's literal + hash. After
// this call, every occurrence of the literal hostname in any string
// passed through Apply is replaced with the 8-hex hash.
//
// Calling with an empty string leaves the hostname rule inert. This
// makes the engine safe on hosts where os.Hostname() returns ""
// (Windows in some chroot-equivalent setups).
//
// The applyFn wired by BuiltinRules already reads HostnameLiteral /
// HostnameHash from the receiver, so SetHostname only has to set the
// fields — no rewiring needed.
func (e *Engine) SetHostname(name string) {
	if name == "" {
		return
	}
	hash := HashHostname(name)
	for i := range e.builtin {
		if e.builtin[i].ID == "hostname" {
			e.builtin[i].HostnameLiteral = name
			e.builtin[i].HostnameHash = hash
			return
		}
	}
}

// AddRegex appends one ad-hoc rule. The replacement string is plain
// text, NOT a regexp template (no $1 expansions); operators using the
// review TUI shouldn't have to know about Go regex syntax for the
// substitution side.
func (e *Engine) AddRegex(pattern, replacement string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("compile %q: %w", pattern, err)
	}
	e.adhoc = append(e.adhoc, Rule{
		ID:      fmt.Sprintf("adhoc_%d", len(e.adhoc)+1),
		Pattern: re,
		applyFn: func(r Rule, s string) string {
			return r.Pattern.ReplaceAllLiteralString(s, replacement)
		},
	})
	return nil
}

// Apply runs every built-in rule (in declaration order) followed by
// every ad-hoc rule (in addition order). Output of one rule feeds
// the next. APRS callsigns are not specifically rule-protected — the
// rule set is engineered so no rule matches the callsign shape, and
// TestEngine_PreservesAPRSCallsigns guards that invariant.
func (e *Engine) Apply(s string) string {
	for _, r := range e.builtin {
		s = r.Apply(s)
	}
	for _, r := range e.adhoc {
		s = r.Apply(s)
	}
	return s
}
