package logbuffer

import (
	"encoding/json"
	"time"
)

// Record is one persisted log row, decoded for read consumers.
type Record struct {
	Time      time.Time
	Level     string
	Component string
	Message   string
	Attrs     map[string]any
}

// QueryOptions filters a Query. The zero value returns the most recent
// rows up to the default limit.
type QueryOptions struct {
	// Since, when non-zero, restricts results to rows at or after this time.
	Since time.Time
	// Limit caps the number of (most-recent) rows returned. Values <= 0
	// fall back to defaultQueryLimit.
	Limit int
	// Levels, when non-empty, restricts results to these slog level
	// strings ("DEBUG","INFO","WARN","ERROR"). Empty means all levels.
	Levels []string
}

const (
	defaultQueryLimit = 250
	maxQueryLimit     = 5000
)

// Query returns persisted log rows in ascending id (chronological)
// order. It selects the most-recent rows matching the filters (newest
// first) then reverses so callers can append to a chronological view.
//
// The read shares the single pooled connection with the slog writer
// (db.go pins MaxOpenConns(1)); WAL mode keeps this correct, and the
// per-poll read volume is tiny, so contention is not a concern.
func (d *DB) Query(opts QueryOptions) ([]Record, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}

	q := "SELECT ts_ns, level, component, msg, attrs_json FROM logs"
	var args []any
	var where []string
	if !opts.Since.IsZero() {
		where = append(where, "ts_ns >= ?")
		args = append(args, opts.Since.UnixNano())
	}
	if len(opts.Levels) > 0 {
		ph := ""
		for i, lv := range opts.Levels {
			if i > 0 {
				ph += ","
			}
			ph += "?"
			args = append(args, lv)
		}
		where = append(where, "level IN ("+ph+")")
	}
	for i, w := range where {
		if i == 0 {
			q += " WHERE "
		} else {
			q += " AND "
		}
		q += w
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.gorm.Raw(q, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var (
			tsNs             int64
			level, comp, msg string
			attrsJSON        string
		)
		if err := rows.Scan(&tsNs, &level, &comp, &msg, &attrsJSON); err != nil {
			return nil, err
		}
		rec := Record{
			Time:      time.Unix(0, tsNs).UTC(),
			Level:     level,
			Component: comp,
			Message:   msg,
		}
		if attrsJSON != "" && attrsJSON != "null" {
			_ = json.Unmarshal([]byte(attrsJSON), &rec.Attrs)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse DESC -> ascending chronological.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
