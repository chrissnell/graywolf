package flareschema

// LogEntry mirrors one row of the logbuffer ring (graywolf-logs.db).
// The shape matches pkg/logbuffer's persisted columns one-for-one:
// ts_ns is unix nanoseconds, Level is "DEBUG"|"INFO"|"WARN"|"ERROR",
// Component is the dotted slog group chain.
//
// Attrs is map[string]any (not json.RawMessage) so the operator UI can
// render it as a key/value table without re-parsing JSON. Empty maps are
// dropped via omitempty so a record with no attrs doesn't carry an extra
// "attrs":{} on the wire.
type LogEntry struct {
	TsNs      int64          `json:"ts_ns"`
	Level     string         `json:"level"`
	Component string         `json:"component"`
	Msg       string         `json:"msg"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

// LogsSection is the top-level "logs" object on a flare. Source documents
// where the rows came from ("graywolf-logs.db", "journalctl",
// "Console.app", "EventLog") so a future operator can identify the
// collector path even after schema evolution.
type LogsSection struct {
	Source  string           `json:"source"`
	Entries []LogEntry       `json:"entries"`
	Issues  []CollectorIssue `json:"issues,omitempty"`
}
