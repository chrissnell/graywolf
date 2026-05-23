package webapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// AvailableUsbSerialDevice is the wire representation of a single attached,
// serial-capable USB device returned by
// GET /api/kiss/available-usb-serial-devices. VidPid is lowercase hex
// "vid:pid" ("2341:0043"); HasPermission reflects whether the Android USB
// permission has been granted for the device.
type AvailableUsbSerialDevice struct {
	VidPid        string `json:"vid_pid"`
	Product       string `json:"product"`
	Manufacturer  string `json:"manufacturer"`
	HasPermission bool   `json:"has_permission"`
}

// AvailableUsbSerialDevicesResponse is the JSON payload returned by
// GET /api/kiss/available-usb-serial-devices. Devices is always a JSON array
// (never null).
type AvailableUsbSerialDevicesResponse struct {
	Devices []AvailableUsbSerialDevice `json:"devices"`
}

// AvailableUsbSerialDevicesSource is the narrow surface the handler consumes.
// The Android build wires this through the platformsvc client (see
// pkg/app/usbserialsource_android.go); desktop builds leave it nil and the
// handler returns 501.
type AvailableUsbSerialDevicesSource interface {
	AvailableUsbSerialDevices(ctx context.Context) ([]AvailableUsbSerialDevice, error)
}

// SetUsbSerialSource installs the USB-serial enumeration source
// post-construction. Called from pkg/app on Android builds; nil elsewhere.
func (s *Server) SetUsbSerialSource(src AvailableUsbSerialDevicesSource) { s.usbSerialSource = src }

// handleGetAvailableUsbSerialDevices returns the attached serial-capable USB
// devices visible to the Android platform service. 501 on non-Android (no
// source wired); {"devices":[...]} on Android, empty array never null.
//
// @Summary  List attached USB serial devices (Android only)
// @Tags     kiss
// @ID       getAvailableUsbSerialDevices
// @Produce  json
// @Success  200 {object} AvailableUsbSerialDevicesResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  501 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /kiss/available-usb-serial-devices [get]
func (s *Server) handleGetAvailableUsbSerialDevices(w http.ResponseWriter, r *http.Request) {
	if s.usbSerialSource == nil {
		writeJSON(w, http.StatusNotImplemented, webtypes.ErrorResponse{
			Error: "USB serial is only available on the Android platform service",
		})
		return
	}
	devs, err := s.usbSerialSource.AvailableUsbSerialDevices(r.Context())
	if err != nil {
		s.internalError(w, r, "list usb serial devices", err)
		return
	}
	out := AvailableUsbSerialDevicesResponse{Devices: devs}
	if out.Devices == nil {
		// Never serialize null — operators / UI clients always expect [].
		out.Devices = []AvailableUsbSerialDevice{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
