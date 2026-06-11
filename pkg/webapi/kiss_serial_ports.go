package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/gps"
)

// listAvailableKissSerialPorts returns the host serial ports the OS can see,
// so the Kiss page can offer a "detected ports" dropdown for the desktop
// `serial` interface type (Windows COM*, Linux /dev/tty*, macOS cu.*). It
// reuses gps.EnumerateSerialPorts — the same pure-Go enumeration the GPS
// page uses — so the two pickers stay consistent. Enumeration failures
// degrade to an empty list (never null) rather than an error: the operator
// can always fall back to typing the port path manually.
//
// @Summary  List available host serial ports
// @Tags     kiss
// @ID       listAvailableKissSerialPorts
// @Produce  json
// @Success  200 {array}  gps.SerialPortInfo
// @Security CookieAuth
// @Router   /kiss/available-serial-ports [get]
func (s *Server) listAvailableKissSerialPorts(w http.ResponseWriter, r *http.Request) {
	ports, err := gps.EnumerateSerialPorts()
	if err != nil {
		s.logger.Warn("enumerate kiss serial ports", "err", err)
		writeJSON(w, http.StatusOK, []gps.SerialPortInfo{})
		return
	}
	if ports == nil {
		ports = []gps.SerialPortInfo{}
	}
	writeJSON(w, http.StatusOK, ports)
}
