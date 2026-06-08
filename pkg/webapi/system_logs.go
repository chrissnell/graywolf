package webapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/logbuffer"
)

// SystemLogSource is the read side of the slog ring buffer. *logbuffer.DB
// satisfies it; tests use a fake. A nil source means the buffer is
// disabled (logbuffer failed to open at startup) — the handler then
// reports available:false instead of erroring.
type SystemLogSource interface {
	Query(opts logbuffer.QueryOptions) ([]logbuffer.Record, error)
}

// SystemLogEntry is one rendered log line.
type SystemLogEntry struct {
	// Timestamp is the RFC3339 (UTC) time the record was emitted.
	Timestamp string `json:"timestamp"`
	// Level is the slog level: DEBUG, INFO, WARN, or ERROR.
	Level string `json:"level"`
	// Component is the slog "component" group (e.g. "webapi"); omitted when unset.
	Component string `json:"component,omitempty"`
	// Message is the log message text.
	Message string `json:"message"`
	// Attrs are the structured key/value attributes attached to the record; omitted when none.
	Attrs map[string]any `json:"attrs,omitempty"`
}

// SystemLogsResponse is the GET /api/system-logs body.
type SystemLogsResponse struct {
	// Logs are the matching records in ascending chronological order.
	Logs []SystemLogEntry `json:"logs"`
	// Available is false when the log buffer is disabled; Logs is then empty.
	Available bool `json:"available"`
	// Cursor is the RFC3339 timestamp of the newest returned record, for incremental `since` polling; omitted when empty.
	Cursor string `json:"cursor,omitempty"`
}

// RegisterSystemLogs installs GET /api/system-logs. Like RegisterPackets
// it is out-of-band (called from wiring after RegisterRoutes) so it owns
// its route without a ServeMux duplicate-pattern panic. src may be nil.
func RegisterSystemLogs(srv *Server, mux *http.ServeMux, src SystemLogSource) {
	_ = srv // kept for signature consistency with other RegisterXxx
	mux.HandleFunc("GET /api/system-logs", listSystemLogs(src))
}

// listSystemLogs returns recent daemon log records from the slog ring
// buffer. By default only INFO-and-above are returned (matching the
// console); pass level=debug to include DEBUG.
//
// @Summary  List system logs
// @Tags     system-logs
// @ID       listSystemLogs
// @Produce  json
// @Param    limit query int    false "Cap result count (non-negative; default 250)"
// @Param    since query string false "Only records at or after this RFC3339 timestamp"
// @Param    level query string false "Minimum level: 'debug' includes everything; default is info"
// @Success  200 {object} webapi.SystemLogsResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /system-logs [get]
func listSystemLogs(src SystemLogSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var opts logbuffer.QueryOptions

		if s := q.Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				badRequest(w, "bad limit")
				return
			}
			opts.Limit = n
		}
		if s := q.Get("since"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				badRequest(w, "bad since (expected RFC3339)")
				return
			}
			opts.Since = t
		}
		// Default to INFO-and-above; level=debug widens to all levels.
		if strings.EqualFold(q.Get("level"), "debug") {
			opts.Levels = nil
		} else {
			opts.Levels = []string{"INFO", "WARN", "ERROR"}
		}

		resp := SystemLogsResponse{Logs: []SystemLogEntry{}}
		if src != nil {
			recs, err := src.Query(opts)
			if err != nil {
				badRequest(w, "log query failed")
				return
			}
			resp.Available = true
			for _, rc := range recs {
				resp.Logs = append(resp.Logs, SystemLogEntry{
					Timestamp: rc.Time.UTC().Format(time.RFC3339),
					Level:     rc.Level,
					Component: rc.Component,
					Message:   rc.Message,
					Attrs:     rc.Attrs,
				})
			}
			if n := len(resp.Logs); n > 0 {
				resp.Cursor = resp.Logs[n-1].Timestamp
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
