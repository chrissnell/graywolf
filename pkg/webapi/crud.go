package webapi

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/pttdevice"
)

// --- PTT ---

func (s *Server) handlePttCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListPttConfigs()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var p configstore.PttConfig
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.UpsertPttConfig(&p); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.notifyBridgeForChannel(r.Context(), p.ChannelID)
		writeJSON(w, http.StatusCreated, p)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePttByChannel(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/ptt/")
	if rest == "available" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePttAvailable(w, r)
		return
	}
	id, err := parseID(rest)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid channel id"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		p, err := s.store.GetPttConfigForChannel(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPut:
		var p configstore.PttConfig
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		p.ChannelID = id
		if err := s.store.UpsertPttConfig(&p); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.notifyBridgeForChannel(r.Context(), id)
		writeJSON(w, http.StatusOK, p)
	case http.MethodDelete:
		if err := s.store.DeletePttConfig(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.notifyBridgeForChannel(r.Context(), id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- TX Timing ---

func (s *Server) handleTxTimingCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		timings, err := s.store.ListTxTimings()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, timings)
	case http.MethodPost:
		var t configstore.TxTiming
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.UpsertTxTiming(&t); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.notifyBridgeForChannel(r.Context(), t.Channel)
		writeJSON(w, http.StatusCreated, t)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTxTimingByChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/tx-timing/"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid channel id"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		t, err := s.store.GetTxTiming(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, t)
	case http.MethodPut:
		var t configstore.TxTiming
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		t.Channel = id
		if err := s.store.UpsertTxTiming(&t); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.notifyBridgeForChannel(r.Context(), id)
		writeJSON(w, http.StatusOK, t)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- KISS ---

// kissRequest matches the JSON the frontend sends for KISS interfaces.
// The frontend uses tcp_port (int) rather than listen_addr (host:port string).
type kissRequest struct {
	Type         string `json:"type"`
	TcpPort      int    `json:"tcp_port"`
	SerialDevice string `json:"serial_device"`
	BaudRate     uint32 `json:"baud_rate"`
	Channel      uint32 `json:"channel"`
}

func kissRequestToModel(req kissRequest) configstore.KissInterface {
	ch := req.Channel
	if ch == 0 {
		ch = 1
	}
	ki := configstore.KissInterface{
		InterfaceType: req.Type,
		Device:        req.SerialDevice,
		BaudRate:      req.BaudRate,
		Channel:       ch,
		Enabled:       true,
		Broadcast:     true,
	}
	if req.Type == "tcp" && req.TcpPort > 0 {
		ki.ListenAddr = fmt.Sprintf("0.0.0.0:%d", req.TcpPort)
		ki.Name = fmt.Sprintf("kiss-tcp-%d", req.TcpPort)
	} else if req.SerialDevice != "" {
		ki.Name = fmt.Sprintf("kiss-serial-%s", req.SerialDevice)
	}
	return ki
}

// notifyKissManager starts or restarts the KISS server for the given
// interface. For non-TCP or disabled interfaces the server is stopped.
func (s *Server) notifyKissManager(ki configstore.KissInterface) {
	if s.kissManager == nil {
		return
	}
	if !ki.Enabled || ki.InterfaceType != "tcp" || ki.ListenAddr == "" {
		s.kissManager.Stop(ki.ID)
		return
	}
	ch := ki.Channel
	if ch == 0 {
		ch = 1
	}
	s.kissManager.Start(s.kissCtx, ki.ID, kiss.ServerConfig{
		Name:       ki.Name,
		ListenAddr: ki.ListenAddr,
		Logger:     s.logger,
		ChannelMap: map[uint8]uint32{0: ch},
		Broadcast:  ki.Broadcast,
	})
}

func kissModelToResponse(ki configstore.KissInterface) map[string]any {
	resp := map[string]any{
		"id":            ki.ID,
		"type":          ki.InterfaceType,
		"serial_device": ki.Device,
		"baud_rate":     ki.BaudRate,
		"channel":       ki.Channel,
		"tcp_port":      0,
	}
	if ki.ListenAddr != "" {
		if _, portStr, err := net.SplitHostPort(ki.ListenAddr); err == nil {
			if p, err := strconv.Atoi(portStr); err == nil {
				resp["tcp_port"] = p
			}
		}
	}
	return resp
}

func (s *Server) handleKissCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListKissInterfaces()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, len(list))
		for i, ki := range list {
			out[i] = kissModelToResponse(ki)
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var req kissRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		k := kissRequestToModel(req)
		if err := s.store.CreateKissInterface(&k); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.notifyKissManager(k)
		writeJSON(w, http.StatusCreated, kissModelToResponse(k))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleKissItem(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/kiss/"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		k, err := s.store.GetKissInterface(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, kissModelToResponse(*k))
	case http.MethodPut:
		var req kissRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		k := kissRequestToModel(req)
		k.ID = id
		if err := s.store.UpdateKissInterface(&k); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.notifyKissManager(k)
		writeJSON(w, http.StatusOK, kissModelToResponse(k))
	case http.MethodDelete:
		if err := s.store.DeleteKissInterface(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if s.kissManager != nil {
			s.kissManager.Stop(id)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- AGW (singleton) ---

func (s *Server) handleAgw(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetAgwConfig()
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, c)
	case http.MethodPut:
		var c configstore.AgwConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.UpsertAgwConfig(&c); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, c)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- iGate config (singleton) + filters ---

func (s *Server) handleIgateConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetIGateConfig()
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, c)
	case http.MethodPut:
		var c configstore.IGateConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.UpsertIGateConfig(&c); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, c)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleIgateFilters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListIGateRfFilters()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var f configstore.IGateRfFilter
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.CreateIGateRfFilter(&f); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, f)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleIgateFilter(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/igate/filters/"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	switch r.Method {
	case http.MethodPut:
		var f configstore.IGateRfFilter
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		f.ID = id
		if err := s.store.UpdateIGateRfFilter(&f); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, f)
	case http.MethodDelete:
		if err := s.store.DeleteIGateRfFilter(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Digipeater config (singleton) + rules ---

func (s *Server) handleDigipeaterConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetDigipeaterConfig()
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, c)
	case http.MethodPut:
		var c configstore.DigipeaterConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.UpsertDigipeaterConfig(&c); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, c)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDigipeaterRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListDigipeaterRules()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var rule configstore.DigipeaterRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.CreateDigipeaterRule(&rule); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, rule)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDigipeaterRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/digipeater/rules/"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	switch r.Method {
	case http.MethodPut:
		var rule configstore.DigipeaterRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		rule.ID = id
		if err := s.store.UpdateDigipeaterRule(&rule); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rule)
	case http.MethodDelete:
		if err := s.store.DeleteDigipeaterRule(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- PTT available devices ---

func (s *Server) handlePttAvailable(w http.ResponseWriter, _ *http.Request) {
	devs := pttdevice.Enumerate()
	writeJSON(w, http.StatusOK, devs)
}

// --- GPS (singleton) ---

func (s *Server) handleGps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetGPSConfig()
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, c)
	case http.MethodPut:
		var c configstore.GPSConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		// Derive Enabled from SourceType so the UI doesn't need a toggle.
		c.Enabled = c.SourceType != "" && c.SourceType != "none"
		if err := s.store.UpsertGPSConfig(&c); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if s.gpsReload != nil {
			select {
			case s.gpsReload <- struct{}{}:
			default:
			}
		}
		writeJSON(w, http.StatusOK, c)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
