package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeUsbSerialSource struct {
	devs []AvailableUsbSerialDevice
	err  error
}

func (f *fakeUsbSerialSource) AvailableUsbSerialDevices(_ context.Context) ([]AvailableUsbSerialDevice, error) {
	return f.devs, f.err
}

func TestGetAvailableUsbSerialDevices_Android_ReturnsList(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetUsbSerialSource(&fakeUsbSerialSource{devs: []AvailableUsbSerialDevice{
		{VidPid: "2341:0043", Product: "TH-D75", Manufacturer: "Kenwood", HasPermission: true},
	}})

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/kiss/available-usb-serial-devices", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp AvailableUsbSerialDevicesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Devices) != 1 || resp.Devices[0].VidPid != "2341:0043" {
		t.Fatalf("devices = %+v", resp.Devices)
	}
}

func TestGetAvailableUsbSerialDevices_NonAndroid_Returns501(t *testing.T) {
	srv, _ := newTestServer(t) // no source wired
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/kiss/available-usb-serial-devices", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestGetAvailableUsbSerialDevices_EmptyList_ReturnsJSONArray(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetUsbSerialSource(&fakeUsbSerialSource{devs: nil})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/kiss/available-usb-serial-devices", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, `"devices":null`) {
		t.Fatalf("expected empty array on wire, got null: %s", body)
	}
	if !strings.Contains(body, `"devices":[]`) {
		t.Fatalf("expected empty array on wire, got: %s", body)
	}
	var resp AvailableUsbSerialDevicesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Devices == nil {
		t.Fatal("devices serialized as null; want []")
	}
}
