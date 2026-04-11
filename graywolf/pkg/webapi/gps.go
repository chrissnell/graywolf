package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerGps(mux *http.ServeMux) {
	mux.HandleFunc("/api/gps", s.handleGps)
	mux.HandleFunc("/api/gps/available", s.handleGpsAvailable)
}

// GET/PUT /api/gps — singleton.
func (s *Server) handleGps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetGPSConfig(r.Context())
		if err != nil || c == nil {
			notFound(w)
			return
		}
		writeJSON(w, http.StatusOK, dto.GPSFromModel(*c))
	case http.MethodPut:
		req, err := decodeJSON[dto.GPSRequest](r)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		if err := req.Validate(); err != nil {
			badRequest(w, err.Error())
			return
		}
		m := req.ToModel()
		if err := s.store.UpsertGPSConfig(r.Context(), &m); err != nil {
			s.internalError(w, r, "upsert gps config", err)
			return
		}
		s.signalGpsReload()
		writeJSON(w, http.StatusOK, dto.GPSFromModel(m))
	default:
		methodNotAllowed(w)
	}
}

// GET /api/gps/available — list of serial ports the OS can see.
func (s *Server) handleGpsAvailable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	ports, err := gps.EnumerateSerialPorts()
	if err != nil {
		s.logger.Warn("enumerate serial ports", "err", err)
		writeJSON(w, http.StatusOK, []gps.SerialPortInfo{})
		return
	}
	writeJSON(w, http.StatusOK, ports)
}

// signalGpsReload performs a non-blocking send on the GPS reload
// channel; coalesces if a previous signal is still buffered.
func (s *Server) signalGpsReload() {
	if s.gpsReload == nil {
		return
	}
	select {
	case s.gpsReload <- struct{}{}:
	default:
	}
}
