package webapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/chrissnell/graywolf/pkg/packetlog"
)

// RegisterPackets installs a real GET /api/packets handler backed by
// the supplied packetlog.Log onto mux. It replaces the stub registered
// by Server.RegisterRoutes — call this after RegisterRoutes.
//
// NOTE: because net/http ServeMux panics on duplicate pattern
// registration, the orchestrator must remove the
//
//	mux.HandleFunc("/api/packets", s.stub("packets"))
//
// line from webapi.go before merging this worktree. See
// scratch/phase4-4c-report.md.
//
// Query parameters:
//
//	since=RFC3339  only entries at or after this timestamp
//	source=KIND    filter by Entry.Source
//	type=TYPE      filter by Entry.Type (APRS packet type)
//	direction=RX|TX|IS
//	channel=N      filter by channel
//	limit=N        cap result count
func RegisterPackets(srv *Server, log *packetlog.Log) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("/api/packets", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			q := r.URL.Query()
			f := packetlog.Filter{
				Source:    q.Get("source"),
				Type:      q.Get("type"),
				Direction: packetlog.Direction(q.Get("direction")),
				Channel:   -1,
			}
			if s := q.Get("since"); s != "" {
				t, err := time.Parse(time.RFC3339, s)
				if err != nil {
					http.Error(w, "bad since: "+err.Error(), http.StatusBadRequest)
					return
				}
				f.Since = t
			}
			if s := q.Get("channel"); s != "" {
				n, err := strconv.Atoi(s)
				if err != nil {
					http.Error(w, "bad channel", http.StatusBadRequest)
					return
				}
				f.Channel = n
			}
			if s := q.Get("limit"); s != "" {
				n, err := strconv.Atoi(s)
				if err != nil || n < 0 {
					http.Error(w, "bad limit", http.StatusBadRequest)
					return
				}
				f.Limit = n
			}
			entries := log.Query(f)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(entries)
		})
	}
}
