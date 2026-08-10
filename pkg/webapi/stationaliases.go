package webapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

// AliasStore is the persistence interface for user-defined station aliases.
// historydb.DB satisfies this interface; nil is returned by the getter
// when the position log is disabled.
type AliasStore interface {
	GetAliases() (map[string]string, error)
	SetAlias(callsign, alias string) error
	DeleteAlias(callsign string) error
}

// AliasStoreGetter is a function that returns the current AliasStore. It
// returns nil when the position log is disabled, which causes PUT/DELETE
// to respond 503 and GET to return an empty map.
type AliasStoreGetter func() AliasStore

// RegisterStationAliases installs the /api/stations/aliases route tree.
// Called from wiring.go alongside RegisterStations.
//
// Routes:
//
//	GET    /api/stations/aliases           → map[callsign]alias (empty {} when position log off)
//	PUT    /api/stations/aliases/{callsign} → {"alias":"..."} saves/replaces alias; 503 when off
//	DELETE /api/stations/aliases/{callsign} → removes alias; 503 when off
func RegisterStationAliases(_ *Server, mux *http.ServeMux, getter AliasStoreGetter) {
	mux.HandleFunc("GET /api/stations/aliases", listAliases(getter))
	mux.HandleFunc("PUT /api/stations/aliases/{callsign}", putAlias(getter))
	mux.HandleFunc("DELETE /api/stations/aliases/{callsign}", deleteAlias(getter))
}

func listAliases(getter AliasStoreGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := getter()
		if db == nil {
			writeJSON(w, http.StatusOK, map[string]string{})
			return
		}
		m, err := db.GetAliases()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if m == nil {
			m = map[string]string{}
		}
		writeJSON(w, http.StatusOK, m)
	}
}

func putAlias(getter AliasStoreGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := getter()
		if db == nil {
			http.Error(w, "position log is not enabled", http.StatusServiceUnavailable)
			return
		}
		callsign := normalizeCallsign(r.PathValue("callsign"))
		if callsign == "" {
			badRequest(w, "callsign is required")
			return
		}
		var body struct {
			Alias string `json:"alias"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		alias := strings.TrimSpace(body.Alias)
		if len(alias) > 64 {
			badRequest(w, "alias too long (max 64 characters)")
			return
		}
		if err := db.SetAlias(callsign, alias); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"callsign": callsign, "alias": alias})
	}
}

func deleteAlias(getter AliasStoreGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := getter()
		if db == nil {
			http.Error(w, "position log is not enabled", http.StatusServiceUnavailable)
			return
		}
		callsign := normalizeCallsign(r.PathValue("callsign"))
		if callsign == "" {
			badRequest(w, "callsign is required")
			return
		}
		if err := db.DeleteAlias(callsign); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// normalizeCallsign uppercases and trims the callsign, returning empty string
// when the result is blank or longer than 15 characters (APRS-IS limit).
func normalizeCallsign(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) == 0 || len(s) > 15 {
		return ""
	}
	return s
}
