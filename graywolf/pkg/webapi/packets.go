package webapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/chrissnell/graywolf/pkg/packetlog"
)

// RegisterPackets installs a GET /api/packets handler backed by the
// supplied packetlog.Log. Server.RegisterRoutes intentionally omits
// /api/packets so this helper can own the route without triggering a
// net/http ServeMux duplicate-pattern panic.
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
					http.Error(w, "bad since (expected RFC3339)", http.StatusBadRequest)
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
