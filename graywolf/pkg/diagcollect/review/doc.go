// Package review presents a scrubbed flare payload to the user and
// returns their decision (submit / cancel / edit notes / add redaction
// regex / diff). Paginated terminal UI built on plain stdin/stdout —
// no curses dependency.
//
// Keystrokes:
//
//	s   submit
//	c   cancel
//	e   edit notes (triggers a re-prompt at the call site)
//	r   add ad-hoc redaction (triggers a regex prompt at the call site)
//	d   diff (resubmit mode only; no-op in fresh-submit mode)
//	space / enter   advance one page
//	q   alias for cancel
//
// Test contract: every behaviour is exercised through Run by writing
// canned bytes to in and reading bytes from out. No real terminal is
// involved.
package review
