package dto

// AX25TerminalMacro is one toolbar macro stored under MacrosJSON. Label
// is the human-visible button text; Payload is base64-encoded raw
// bytes the operator wants the macro to send (so the macro can carry
// terminal control codes, not just printable text).
type AX25TerminalMacro struct {
	Label   string `json:"label"`
	Payload string `json:"payload"`
}

// AX25TerminalConfig is the GET response shape for
// /api/ax25/terminal-config. Macros is exposed as a typed array; the
// store persists it as a JSON-text column.
type AX25TerminalConfig struct {
	ScrollbackRows uint32              `json:"scrollback_rows"`
	CursorBlink    bool                `json:"cursor_blink"`
	DefaultModulo  uint32              `json:"default_modulo"`
	DefaultPaclen  uint32              `json:"default_paclen"`
	Macros         []AX25TerminalMacro `json:"macros"`
	RawTailFilter  string              `json:"raw_tail_filter"`
}

// AX25TerminalConfigPatch is the PUT body for /api/ax25/terminal-config.
// Every field is a pointer so the handler can distinguish "field absent
// from request" (preserve existing column) from "field present with
// zero value" (validation error). The Macros slice uses nil-vs-empty
// to mean the same: nil = absent, []{} = explicit clear.
type AX25TerminalConfigPatch struct {
	ScrollbackRows *uint32              `json:"scrollback_rows,omitempty"`
	CursorBlink    *bool                `json:"cursor_blink,omitempty"`
	DefaultModulo  *uint32              `json:"default_modulo,omitempty"`
	DefaultPaclen  *uint32              `json:"default_paclen,omitempty"`
	Macros         []AX25TerminalMacro  `json:"macros,omitempty"`
	RawTailFilter  *string              `json:"raw_tail_filter,omitempty"`
}
