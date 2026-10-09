package webapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// BondedBtDevice is the wire representation of a single bonded Bluetooth
// device returned by GET /api/kiss/bonded-bt-devices. MAC is the
// colon-separated uppercase Bluetooth address (e.g. "AA:BB:CC:DD:EE:FF");
// Name is the user-visible label set at bond time and may be empty if the
// device never advertised one.
type BondedBtDevice struct {
	MAC  string `json:"mac"`
	Name string `json:"name"`
}

// BondedBtDevicesResponse is the JSON payload returned by
// GET /api/kiss/bonded-bt-devices. Devices is always serialized as a JSON
// array (never null) — an empty bond set yields []. See
// handleGetBondedBtDevices.
type BondedBtDevicesResponse struct {
	Devices []BondedBtDevice `json:"devices"`
}

// BondedBtDevicesSource is the narrow surface the bonded-BT handler
// consumes. The Android build wires this through the platformsvc client
// (see pkg/app/btsource_android.go) and macOS builds with cgo through
// IOBluetooth (pkg/app/btsource_darwin.go); other builds leave it nil and
// the handler returns 501 Not Implemented, which the UI uses to hide the
// Bluetooth interface type.
type BondedBtDevicesSource interface {
	BondedBtDevices(ctx context.Context) ([]BondedBtDevice, error)
}

// SetBtSource installs the Bluetooth bonded-devices source
// post-construction. Called from pkg/app; remains nil on platforms without
// a Bluetooth transport so the handler returns 501.
func (s *Server) SetBtSource(src BondedBtDevicesSource) { s.btSource = src }

// handleGetBondedBtDevices returns the paired Bluetooth devices visible
// to the Android platform service or, on macOS, to IOBluetooth. Without a
// source it returns 501 Not Implemented; otherwise the response body is
// {"devices": [...]}. An empty bond set returns an empty array, never null.
//
// @Summary  List paired Bluetooth devices (Android and macOS)
// @Tags     kiss
// @ID       getBondedBtDevices
// @Produce  json
// @Success  200 {object} BondedBtDevicesResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  501 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /kiss/bonded-bt-devices [get]
func (s *Server) handleGetBondedBtDevices(w http.ResponseWriter, r *http.Request) {
	if s.btSource == nil {
		writeJSON(w, http.StatusNotImplemented, webtypes.ErrorResponse{
			Error: "Bluetooth is only available on Android and in the macOS build",
		})
		return
	}
	devs, err := s.btSource.BondedBtDevices(r.Context())
	if err != nil {
		s.internalError(w, r, "list bonded bluetooth devices", err)
		return
	}
	out := BondedBtDevicesResponse{Devices: devs}
	if out.Devices == nil {
		// Never serialize null — operators / UI clients always expect [].
		out.Devices = []BondedBtDevice{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
