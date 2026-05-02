package dto

// AX25TerminalMacro is one toolbar macro stored under MacrosJSON. Label
// is the human-visible button text; Payload is base64-encoded raw
// bytes the operator wants the macro to send (so the macro can carry
// terminal control codes, not just printable text).
type AX25TerminalMacro struct {
	Label   string `json:"label"`
	Payload string `json:"payload"`
}

// AX25TerminalConfig is the on-wire shape of GET/PUT
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
