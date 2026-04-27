package webapi

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// DefaultAutocompleteLimit is the cap applied when no explicit limit
// is supplied. Big enough to cover the bot list + a reasonable
// station-history tail without overwhelming the combobox UI.
const DefaultAutocompleteLimit = 25

// emptyInputStationCap caps the station suggestions shown when the
// operator hasn't typed anything yet. The bot group is always fully
// surfaced regardless of this cap.
const emptyInputStationCap = 5

// RegisterStationsAutocomplete installs GET /api/stations/autocomplete.
// Server.RegisterRoutes intentionally omits this route so the wiring
// layer can register it only after all three sources (bot directory,
// station cache, messages store) are constructed.
//
// The autocomplete merges three sources in priority order:
//  1. Bot directory (DefaultBotDirectory or server override)
//  2. Station cache hits
//  3. Message history (peers we've previously exchanged messages with)
//
// When the operator hasn't typed anything, the bot group is rendered
// in full and the station section is capped at emptyInputStationCap
// most-recent entries. When a prefix is supplied, all three sources
// contribute matches (case-insensitive prefix) up to the overall
// limit.
//
// Ranking after merge:
//   - source=="bot"   ahead of everything (always first group)
//   - source=="cache" (or cache+history) ahead of history-only
//   - within each group: last_heard DESC
func RegisterStationsAutocomplete(
	srv *Server,
	mux *http.ServeMux,
	store MessagesStore,
	cache stationcache.StationStore,
) {
	h := &autocompleteHandler{
		srv:   srv,
		store: store,
		cache: cache,
	}
	mux.HandleFunc("GET /api/stations/autocomplete", h.serve)
}

type autocompleteHandler struct {
	srv   *Server
	store MessagesStore
	cache stationcache.StationStore
}

// serve is the HTTP entry point for GET /api/stations/autocomplete.
//
// @Summary  Station autocomplete
// @Tags     messages
// @ID       autocompleteStations
// @Produce  json
// @Param    q     query string false "Prefix (case-insensitive). Empty returns bots + recent stations."
// @Param    limit query int    false "Cap result count (1..100, default 25)"
// @Success  200 {array}  dto.StationAutocomplete
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /stations/autocomplete [get]
func (h *autocompleteHandler) serve(w http.ResponseWriter, r *http.Request) {
	q := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := DefaultAutocompleteLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			badRequest(w, "bad limit (expected 1..100)")
			return
		}
		limit = n
	}

	dir := h.srv.messagesBotDir
	if dir == nil {
		dir = messages.DefaultBotDirectory
	}

	// --- source 1: bots (always included, ignores limit) ---
	var bots []dto.StationAutocomplete
	var botSuggestions []messages.BotAddress
	if q == "" {
		botSuggestions = dir.List()
	} else {
		botSuggestions = dir.Match(q)
	}
	for _, b := range botSuggestions {
		bots = append(bots, dto.StationAutocomplete{
			Callsign:    b.Callsign,
			Source:      "bot",
			Description: b.Description,
		})
	}

	// --- source 2: station cache (prefix match) ---
	cacheHits := map[string]time.Time{}
	if h.cache != nil {
		for _, s := range queryCacheByPrefix(h.cache, q) {
			cacheHits[strings.ToUpper(s.Callsign)] = s.LastHeard
		}
	}

	// --- source 3: message history (prefix match) ---
	historyHits := map[string]time.Time{}
	if h.store != nil {
		rows, _ := h.store.QueryMessageHistoryByPeer(r.Context(), q, limit*2)
		for _, row := range rows {
			historyHits[strings.ToUpper(row.Callsign)] = row.LastHeard
		}
	}

	// Merge cache + history by callsign; source string reflects which
	// sources contributed. Last-heard wins when both report a time.
	type stationEntry struct {
		callsign  string
		lastHeard time.Time
		inCache   bool
		inHistory bool
	}
	merged := map[string]*stationEntry{}
	for call, t := range cacheHits {
		merged[call] = &stationEntry{callsign: call, lastHeard: t, inCache: true}
	}
	for call, t := range historyHits {
		if e, ok := merged[call]; ok {
			e.inHistory = true
			if t.After(e.lastHeard) {
				e.lastHeard = t
			}
		} else {
			merged[call] = &stationEntry{callsign: call, lastHeard: t, inHistory: true}
		}
	}
	// Exclude any station that collides with a bot name — the bot
	// entry already covers it and carries a helpful description.
	for call := range merged {
		if messages.IsWellKnownBot(call) {
			delete(merged, call)
		}
	}

	entries := make([]*stationEntry, 0, len(merged))
	for _, e := range merged {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.inCache != b.inCache {
			return a.inCache // cache ahead of history
		}
		if a.inHistory != b.inHistory {
			return a.inHistory // history ahead of cache-only (shouldn't happen after in-cache tie)
		}
		return a.lastHeard.After(b.lastHeard)
	})

	// Cap the station slice. Empty-input gets a tighter cap; a typed
	// prefix gets the full limit.
	stationCap := limit
	if q == "" {
		stationCap = emptyInputStationCap
	}
	if len(entries) > stationCap {
		entries = entries[:stationCap]
	}

	stations := make([]dto.StationAutocomplete, 0, len(entries))
	for _, e := range entries {
		src := "cache"
		switch {
		case e.inCache && e.inHistory:
			src = "cache+history"
		case e.inHistory && !e.inCache:
			src = "history"
		}
		stations = append(stations, dto.StationAutocomplete{
			Callsign:  e.callsign,
			Source:    src,
			LastHeard: e.lastHeard.UTC().Format(time.RFC3339),
		})
	}

	// Compose final response: bots first, then stations. Overall cap
	// applies to the concatenation but always keeps the full bot set
	// (per plan: "source='bot' DESC" — bots always lead).
	out := append(bots, stations...)
	if len(out) > limit && q != "" {
		// Only truncate the tail — never truncate bots. If the bots
		// group alone exceeds the limit the user gets all of them.
		if len(bots) >= limit {
			out = bots
		} else {
			out = out[:limit]
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// queryCacheByPrefix walks the station cache for matches. MemCache's
// QueryBBox is spatial; for prefix lookups we'd want a string-index,
// but the cache doesn't expose one. The simplest correct-in-practice
// approach: scan the cache via a global-bbox query and filter in the
// handler. Real deployments have a few thousand stations at most;
// the O(n) walk is cheap relative to SQLite hits.
//
// An empty prefix returns every cached station.
func queryCacheByPrefix(cache stationcache.StationStore, prefix string) []stationcache.Station {
	if cache == nil {
		return nil
	}
	// Global bbox — the equator through the poles, full longitude span.
	// Lookback window of 24h: beyond that the cache evicts anyway, and
	// the autocomplete should surface recent contacts, not a full
	// historical dump.
	stations := cache.QueryBBox(stationcache.BBox{
		SwLat: -90, SwLon: -180,
		NeLat: 90, NeLon: 180,
	}, 24*time.Hour)
	if prefix == "" {
		return stations
	}
	up := strings.ToUpper(prefix)
	out := stations[:0]
	for _, s := range stations {
		if strings.HasPrefix(strings.ToUpper(s.Callsign), up) {
			out = append(out, s)
		}
	}
	return out
}
