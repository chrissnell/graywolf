package diagcollect

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/glebarez/sqlite"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// LogDBPath returns the path of the graywolf-logs.db that should be
// read. configDBPath is the resolved graywolf.db path — the log DB
// lives next to it on disk installs, and on tmpfs locations on
// Pi/SD-card.
func LogDBPath(configDBPath string) (string, bool) {
	candidates := []string{}
	if configDBPath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(configDBPath), "graywolf-logs.db"))
	}
	candidates = append(candidates,
		"/run/graywolf/graywolf-logs.db",
		"/dev/shm/graywolf/graywolf-logs.db",
	)
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, true
		}
	}
	return "", false
}

// CollectLogs is the production entry point: discover the log DB,
// read the last N rows ordered ts_ns ASC.
func CollectLogs(configDBPath string, limit int) flareschema.LogsSection {
	path, ok := LogDBPath(configDBPath)
	if !ok {
		return flareschema.LogsSection{
			Source:  "graywolf-logs.db",
			Entries: []flareschema.LogEntry{},
			Issues: []flareschema.CollectorIssue{{
				Kind:    "log_db_unavailable",
				Message: "graywolf-logs.db not found next to configstore or in tmpfs locations",
			}},
		}
	}
	return collectLogsAt(path, limit)
}

func collectLogsAt(path string, limit int) flareschema.LogsSection {
	out := flareschema.LogsSection{
		Source:  "graywolf-logs.db",
		Entries: []flareschema.LogEntry{},
	}
	if limit <= 0 {
		limit = 5000
	}
	if _, err := os.Stat(path); err != nil {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind: "log_db_unavailable", Message: err.Error(), Path: path,
		})
		return out
	}
	db, err := sql.Open("sqlite", path+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind: "log_db_open_failed", Message: err.Error(), Path: path,
		})
		return out
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT id, ts_ns, level, component, msg, attrs_json
		FROM (
			SELECT * FROM logs ORDER BY id DESC LIMIT ?
		)
		ORDER BY id ASC
	`, limit)
	if err != nil {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind: "log_db_query_failed", Message: err.Error(), Path: path,
		})
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id        int64
			ts        int64
			level     string
			component string
			msg       string
			attrsJSON string
		)
		if err := rows.Scan(&id, &ts, &level, &component, &msg, &attrsJSON); err != nil {
			out.Issues = append(out.Issues, flareschema.CollectorIssue{
				Kind: "log_row_scan_failed", Message: err.Error(),
			})
			continue
		}
		var attrs map[string]any
		if attrsJSON != "" {
			if err := json.Unmarshal([]byte(attrsJSON), &attrs); err != nil {
				attrs = map[string]any{"_attrs_decode_error": err.Error()}
			}
		}
		out.Entries = append(out.Entries, flareschema.LogEntry{
			TsNs:      ts,
			Level:     level,
			Component: component,
			Msg:       msg,
			Attrs:     attrs,
		})
	}
	if err := rows.Err(); err != nil {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind: "log_row_iter_failed", Message: err.Error(),
		})
	}
	return out
}

// writeOneLogRow is a test helper used by logs_test.go. Lives in the
// production file (not _test.go) so its signature is referenceable
// from tests in this package without an import cycle.
func writeOneLogRow(path string, tsNs int64, level, component, msg, attrsJSON string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	_, err = db.Exec(
		"INSERT INTO logs (ts_ns, level, component, msg, attrs_json) VALUES (?, ?, ?, ?, ?)",
		tsNs, level, component, msg, attrsJSON,
	)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	return nil
}
