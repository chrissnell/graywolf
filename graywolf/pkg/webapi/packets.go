package webapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/packetlog"
)

// packetDTO enriches a packet log entry with device identification and distance.
type packetDTO struct {
	packetlog.Entry
	// Device is APRS device identification (manufacturer, model) inferred from the TOCALL field; omitted when unknown.
	Device *aprs.DeviceInfo `json:"device,omitempty"`
	// DistanceMi is the great-circle distance from this station's GPS fix to the packet's reported position, in statute miles; omitted when either position is unavailable.
	DistanceMi *float64 `json:"distance_mi,omitempty"`
	// Via is the callsign of the last digipeater that forwarded this packet (H-bit set); empty string for direct packets.
	Via string `json:"via,omitempty"`
}

// RegisterPackets installs a GET /api/packets handler backed by the
// supplied packetlog.Log. Server.RegisterRoutes intentionally omits
// /api/packets so this helper can own the route without triggering a
// net/http ServeMux duplicate-pattern panic.
//
// Signature shape (mux second) is shared with every out-of-band
// RegisterXxx in this package — see RegisterPosition, RegisterIgate,
// RegisterStations. Keep callers consistent.
//
// Operation IDs in the swag annotation blocks below are frozen against
// constants in pkg/webapi/docs/op_ids.go; `make docs-lint` enforces the
// correspondence.
func RegisterPackets(srv *Server, mux *http.ServeMux, log *packetlog.Log, posCache gps.PositionCache) {
	_ = srv // kept in signature for consistency with other RegisterXxx
	mux.HandleFunc("GET /api/packets", listPackets(log, posCache))
}

// listPackets returns recent APRS packets from the in-memory packet log.
// Results are enriched with tocall-derived device info and, when a local
// station position is known, haversine distance from the receiver.
//
// @Summary  List packets
// @Tags     packets
// @ID       listPackets
// @Produce  json
// @Param    since     query string false "Only entries at or after this RFC3339 timestamp"
// @Param    source    query string false "Filter by Entry.Source (e.g. rf, is)"
// @Param    type      query string false "Filter by APRS packet type (Entry.Type)"
// @Param    direction query string false "Filter by direction (RX|TX|IS)"
// @Param    channel   query int    false "Filter by channel number"
// @Param    limit     query int    false "Cap result count (non-negative)"
// @Success  200 {array}  webapi.packetDTO
// @Failure  400 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /packets [get]
func listPackets(log *packetlog.Log, posCache gps.PositionCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := packetlog.Filter{
			Source:    q.Get("source"),
			Type:      q.Get("type"),
			Direction: packetlog.Direction(q.Get("direction")),
			Channel:   -1,
		}
		if s := q.Get("since"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				badRequest(w, "bad since (expected RFC3339)")
				return
			}
			f.Since = t
		}
		if s := q.Get("channel"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil {
				badRequest(w, "bad channel")
				return
			}
			f.Channel = n
		}
		if s := q.Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				badRequest(w, "bad limit")
				return
			}
			f.Limit = n
		}
		entries := log.Query(f)

		// Get our station position for distance calc
		var myLat, myLon float64
		var havePos bool
		if posCache != nil {
			fix, ok := posCache.Get()
			if ok && fix.Latitude != 0 && fix.Longitude != 0 {
				myLat, myLon = fix.Latitude, fix.Longitude
				havePos = true
			}
		}

		out := make([]packetDTO, len(entries))
		for i := range entries {
			out[i].Entry = entries[i]
			enrichPacket(&out[i], havePos, myLat, myLon)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// enrichPacket adds device info and distance to a packet DTO.
func enrichPacket(dto *packetDTO, havePos bool, myLat, myLon float64) {
	d := dto.Decoded
	if d == nil {
		return
	}

	// Device identification from tocall
	if dev := aprs.LookupTocall(d.Dest); dev != nil {
		dto.Device = dev
	} else if d.MicE != nil && d.MicE.Manufacturer != "" {
		// Fall back to mic-e manufacturer string already decoded
		dto.Device = &aprs.DeviceInfo{Model: d.MicE.Manufacturer}
	}

	// Distance calculation
	if !havePos {
		return
	}

	var pktLat, pktLon float64
	var hasPktPos bool

	switch {
	case d.Position != nil:
		pktLat, pktLon = d.Position.Latitude, d.Position.Longitude
		hasPktPos = true
	case d.MicE != nil:
		pktLat, pktLon = d.MicE.Position.Latitude, d.MicE.Position.Longitude
		hasPktPos = true
	case d.Object != nil && d.Object.Position != nil:
		pktLat, pktLon = d.Object.Position.Latitude, d.Object.Position.Longitude
		hasPktPos = true
	case d.Item != nil && d.Item.Position != nil:
		pktLat, pktLon = d.Item.Position.Latitude, d.Item.Position.Longitude
		hasPktPos = true
	}

	if !hasPktPos || (pktLat == 0 && pktLon == 0) {
		return
	}

	dist := aprs.HaversineDistanceMi(myLat, myLon, pktLat, pktLon)
	dto.DistanceMi = &dist

	// Determine via (last digipeater that set H-bit, or direct)
	dto.Via = lastDigipeater(d.Path)
}

// lastDigipeater returns the callsign of the last path element with H-bit set
// (indicated by trailing '*'). Returns "" for direct packets.
func lastDigipeater(path []string) string {
	last := ""
	for _, hop := range path {
		if strings.HasSuffix(hop, "*") {
			call := strings.TrimSuffix(hop, "*")
			// Skip generic aliases like WIDE1-1, RELAY, etc.
			upper := strings.ToUpper(call)
			if strings.HasPrefix(upper, "WIDE") ||
				strings.HasPrefix(upper, "RELAY") ||
				strings.HasPrefix(upper, "TRACE") ||
				strings.HasPrefix(upper, "QA") {
				continue
			}
			last = call
		}
	}
	return last
}
