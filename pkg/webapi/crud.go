package webapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// --- PTT ---

func (s *Server) handlePttCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
}

func (s *Server) handlePttByChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/ptt/"))
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

func (s *Server) handleKissCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.ListKissInterfaces()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var k configstore.KissInterface
		if err := json.NewDecoder(r.Body).Decode(&k); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.CreateKissInterface(&k); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, k)
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
	case http.MethodPut:
		var k configstore.KissInterface
		if err := json.NewDecoder(r.Body).Decode(&k); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		k.ID = id
		if err := s.store.UpdateKissInterface(&k); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, k)
	case http.MethodDelete:
		if err := s.store.DeleteKissInterface(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
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
		if err := s.store.UpsertGPSConfig(&c); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, c)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
