package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/stationcache"
)

type fakeHeatmapStore struct {
	res *stationcache.HeatmapResult
	err error
}

func (f *fakeHeatmapStore) QueryHeatmap(_ time.Duration, _ stationcache.BBox) (*stationcache.HeatmapResult, error) {
	return f.res, f.err
}

func TestHeatmapHandler(t *testing.T) {
	store := &fakeHeatmapStore{res: &stationcache.HeatmapResult{
		Points:      []stationcache.HeatPoint{{Lat: 35.0, Lon: -95.0, Count: 3}},
		MaxCount:    3,
		Unlocatable: 7,
	}}
	mux := http.NewServeMux()
	RegisterHeatmap(nil, mux, store)

	req := httptest.NewRequest(http.MethodGet, "/api/heatmap?bbox=30,-100,40,-90&timerange=3600", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Geometry struct {
				Coordinates [2]float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				Count int `json:"count"`
			} `json:"properties"`
		} `json:"features"`
		Properties struct {
			MaxCount    int `json:"max_count"`
			Unlocatable int `json:"unlocatable"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fc.Type != "FeatureCollection" {
		t.Errorf("type = %q", fc.Type)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("features = %d, want 1", len(fc.Features))
	}
	if fc.Features[0].Geometry.Coordinates != [2]float64{-95.0, 35.0} {
		t.Errorf("coords = %v, want [-95,35] (lon,lat)", fc.Features[0].Geometry.Coordinates)
	}
	if fc.Features[0].Properties.Count != 3 {
		t.Errorf("count = %d, want 3", fc.Features[0].Properties.Count)
	}
	if fc.Properties.MaxCount != 3 || fc.Properties.Unlocatable != 7 {
		t.Errorf("max_count=%d unlocatable=%d, want 3/7", fc.Properties.MaxCount, fc.Properties.Unlocatable)
	}
}

func TestHeatmapHandlerBadBBox(t *testing.T) {
	mux := http.NewServeMux()
	RegisterHeatmap(nil, mux, &fakeHeatmapStore{res: &stationcache.HeatmapResult{}})
	req := httptest.NewRequest(http.MethodGet, "/api/heatmap", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
