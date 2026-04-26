package flareschema

// ConfigItem is one row of the configstore dump after scrubbing. Values
// that the scrubber decided to suppress are written as the literal
// string "[REDACTED]" — that placeholder lives in the value field so the
// operator UI doesn't need a separate "redacted" flag column.
type ConfigItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ConfigSection is the top-level "config" object on a flare. Items
// preserve the order the configstore emitted them so resubmit diffs stay
// stable across re-runs. Issues records collector failures (e.g.
// "configstore unreadable") so a flare without config remains useful for
// system+device debugging.
type ConfigSection struct {
	Items  []ConfigItem     `json:"items"`
	Issues []CollectorIssue `json:"issues,omitempty"`
}
