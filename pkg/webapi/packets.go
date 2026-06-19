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
	// ChannelName is the display name of the channel that handled the packet,
	// resolved from the numeric Channel ID; omitted when the ID maps to no
	// configured channel (e.g. channel 0, used for non-RF / APRS-IS arrivals).
	ChannelName string `json:"channel_name,omitempty"`
	// Device is APRS device identification (manufacturer, model) inferred from the TOCALL field; omitted when unknown.
	Device *aprs.DeviceInfo `json:"device,omitempty"`
	// DistanceMi is the great-circle distance from this station's GPS fix to the packet's reported position, in statute miles; omitted when either position is unavailable.
	DistanceMi *float64 `json:"distance_mi,omitempty"`
	// Via is the callsign of the last digipeater that forwarded this packet (H-bit set); empty string for direct packets.
	Via string `json:"via,omitempty"`
	// Lat/Lon are the packet's reported coordinates in decimal degrees (WGS84),
	// surfaced for any transmission type that carries a fix -- position, Mic-E,
	// weather-with-position, object, and item alike. Omitted for positionless
	// packets (messages, telemetry, status). The web UI uses these to render the
	// click-to-zoom map reticle on a log entry; unlike DistanceMi they do not
	// depend on the local station having its own GPS fix.
	Lat *float64 `json:"lat,omitempty"`
	Lon *float64 `json:"lon,omitempty"`
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
	mux.HandleFunc("GET /api/packets", listPackets(srv, log, posCache))
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
func listPackets(srv *Server, log *packetlog.Log, posCache gps.PositionCache) http.HandlerFunc {
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

		// Resolve channel IDs to display names once per request. The channels
		// table is tiny, so a single ListChannels query is negligible next to
		// the JSON encode below. A nil store (some tests) just skips names.
		var chanNames map[uint32]string
		if srv != nil && srv.store != nil {
			if chs, err := srv.store.ListChannels(r.Context()); err == nil {
				chanNames = make(map[uint32]string, len(chs))
				for _, ch := range chs {
					chanNames[ch.ID] = ch.Name
				}
			}
		}

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
			out[i].ChannelName = chanNames[entries[i].Channel]
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

	// Coordinates: surfaced for every transmission type that carries a fix,
	// independent of whether the local station knows its own position. This is
	// what the web log's click-to-zoom reticle keys off of.
	pktLat, pktLon, hasPktPos := packetPosition(d)
	if !hasPktPos {
		return
	}
	dto.Lat, dto.Lon = &pktLat, &pktLon

	// Determine via (last digipeater that set H-bit, or direct)
	dto.Via = lastDigipeater(d.Path)

	// Distance needs the local fix; coordinates above do not.
	if !havePos {
		return
	}
	dist := aprs.HaversineDistanceMi(myLat, myLon, pktLat, pktLon)
	dto.DistanceMi = &dist
}

// packetPosition returns the reported coordinates for any APRS transmission
// type that carries a fix -- a plain position report, a Mic-E report, an
// object or item with an embedded position, or a weather report (whose fix
// rides on the position field). Positionless packets (messages, telemetry,
// bare status, positionless weather) yield ok=false. The 0/0 "null island"
// guard rejects an unset Position struct so a packet that failed to decode a
// fix doesn't masquerade as one off the African coast.
//
// d.Position is the primary path: the decoder copies the fix there for Mic-E
// (pkg/aprs/mice.go) and positioned weather (pkg/aprs/position.go), so those
// are caught by the first case. The d.MicE/d.Object/d.Item cases are defensive
// fallbacks for synthesized packets or future decoder paths that populate only
// the type-specific struct.
func packetPosition(d *aprs.DecodedAPRSPacket) (lat, lon float64, ok bool) {
	switch {
	case d.Position != nil:
		lat, lon = d.Position.Latitude, d.Position.Longitude
	case d.MicE != nil:
		lat, lon = d.MicE.Position.Latitude, d.MicE.Position.Longitude
	case d.Object != nil && d.Object.Position != nil:
		lat, lon = d.Object.Position.Latitude, d.Object.Position.Longitude
	case d.Item != nil && d.Item.Position != nil:
		lat, lon = d.Item.Position.Latitude, d.Item.Position.Longitude
	default:
		return 0, 0, false
	}
	if lat == 0 && lon == 0 {
		return 0, 0, false
	}
	return lat, lon, true
}

// lastDigipeater returns the callsign of the last path element with H-bit set
// (indicated by trailing '*'). Returns "" for direct packets.
func lastDigipeater(path []string) string {
	last := ""
	for _, hop := range path {
		if strings.HasSuffix(hop, "*") {
			// Skip generic aliases like WIDE1-1, RELAY, etc.
			if aprs.IsGenericPathAlias(hop) {
				continue
			}
			last = strings.TrimSuffix(hop, "*")
		}
	}
	return last
}
