package webapi

import (
	"encoding/json"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestBeaconFreshnessHTTPRoundTripAndOldClientSave(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
		return r
	}
	body := `{"type":"tracker","channel":1,"destination":"APGRWO","interval":600,"require_fresh_gps":true,"gps_max_age_seconds":45}`
	r := request("POST", "/api/beacons", body)
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	var b dto.BeaconResponse
	if err := json.Unmarshal(r.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	path := "/api/beacons/" + strconv.Itoa(int(b.ID))
	r = request("PUT", path, `{"type":"tracker","channel":1,"destination":"APGRWO","interval":600}`)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request("GET", path, "")
	if err := json.Unmarshal(r.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if !b.RequireFreshGPS || b.GPSMaxAgeSeconds != 45 {
		t.Fatalf("older client cleared settings: %+v", b)
	}
	r = request("PUT", path, `{"type":"tracker","channel":1,"require_fresh_gps":false,"gps_max_age_seconds":3601}`)
	if r.Code != 400 {
		t.Fatal("invalid age accepted", r.Code)
	}
}
