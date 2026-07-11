package webapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// HeatmapStore is the read side the heatmap handler needs;
// *stationcache.PersistentCache satisfies it.
type HeatmapStore interface {
	QueryHeatmap(window time.Duration, bbox stationcache.BBox) (*stationcache.HeatmapResult, error)
}

// RegisterHeatmap installs GET /api/heatmap. Signature shape (mux second)
// matches the other RegisterXxx helpers in this package.
func RegisterHeatmap(srv *Server, mux *http.ServeMux, store HeatmapStore) {
	_ = srv // kept for consistency with other RegisterXxx
	mux.HandleFunc("GET /api/heatmap", heatmapHandler(store))
}

func heatmapHandler(store HeatmapStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		bbox, err := parseBBox(q.Get("bbox"))
		if err != nil {
			badRequest(w, err.Error())
			return
		}

		timerange := 3600 * time.Second
		if s := q.Get("timerange"); s != "" {
			secs, err := strconv.Atoi(s)
			if err != nil || secs <= 0 {
				badRequest(w, "timerange must be a positive integer (seconds)")
				return
			}
			timerange = time.Duration(secs) * time.Second
		}

		res, err := store.QueryHeatmap(timerange, bbox)
		if err != nil {
			http.Error(w, "heatmap query failed", http.StatusInternalServerError)
			return
		}
		if res == nil {
			res = &stationcache.HeatmapResult{}
		}

		type geometry struct {
			Type        string     `json:"type"`
			Coordinates [2]float64 `json:"coordinates"`
		}
		type feature struct {
			Type       string         `json:"type"`
			Geometry   geometry       `json:"geometry"`
			Properties map[string]int `json:"properties"`
		}
		features := make([]feature, 0, len(res.Points))
		for _, p := range res.Points {
			features = append(features, feature{
				Type:       "Feature",
				Geometry:   geometry{Type: "Point", Coordinates: [2]float64{p.Lon, p.Lat}},
				Properties: map[string]int{"count": p.Count},
			})
		}
		body := map[string]any{
			"type":     "FeatureCollection",
			"features": features,
			"properties": map[string]int{
				"max_count":   res.MaxCount,
				"unlocatable": res.Unlocatable,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
}
