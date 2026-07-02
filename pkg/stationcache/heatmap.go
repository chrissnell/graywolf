package stationcache

import (
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// HeatPoint is one aggregated heatmap bucket: a coordinate and the total
// number of directly-received packets attributed to it in the query window.
type HeatPoint struct {
	Lat   float64
	Lon   float64
	Count int
}

// HeatmapResult is the aggregate returned by a heatmap query. Points are the
// located buckets, MaxCount is the largest single-bucket count (for client-side
// weight normalization), and Unlocatable counts packets whose attributed
// transmitter had no known position in the window.
type HeatmapResult struct {
	Points      []HeatPoint
	MaxCount    int
	Unlocatable int
}

// RxEvent is one directly-received-packet reception, ready to persist for the
// heatmap. It is recorded once per physical RF frame at the ingest edge, not
// per station-cache entry, so a single frame counts once regardless of how
// many cache entries it produces. AttrKey is the station key of the last RF
// transmitter (origin for direct, last-hop digipeater for digipeated); Lat/Lon
// are set only when HasPos is true, otherwise the position is resolved at query
// time from AttrKey's latest known fix.
type RxEvent struct {
	Timestamp time.Time
	AttrKey   string
	Hops      int
	Lat       float64
	Lon       float64
	HasPos    bool
}

// lastHBitDigi returns the callsign of the last used (H-bit, "*"-suffixed)
// real digipeater in path, or "" if there is none. Generic routing aliases
// (WIDE/RELAY/TRACE/q-constructs) are skipped even when flagged used, matching
// aprs.CountHops — a used alias rides alongside the digipeater that consumed it
// rather than being a hop of its own, so the meaningful last transmitter is the
// last non-alias "*" entry.
func lastHBitDigi(path []string) string {
	last := ""
	for _, hop := range path {
		if !strings.HasSuffix(hop, "*") || aprs.IsGenericPathAlias(hop) {
			continue
		}
		last = strings.TrimSuffix(hop, "*")
	}
	return last
}

// BuildRxEvent derives the heatmap reception event for a directly-received RF
// frame, returning ok=false when the frame should not contribute heat. It is
// the single source of truth for heatmap attribution:
//
//   - Internet-to-RF gated frames (third-party) never count — nothing of that
//     station was heard on our radio.
//   - Digipeated frames (a last H-bit "*" digipeater in the path) are
//     attributed to that digipeater's station key with HasPos=false; the
//     originating station's coordinates are deliberately not used.
//   - Direct frames are attributed to the origin, carrying the packet's own
//     coordinates when present. Positionless direct frames (messages, status)
//     still count, resolved at query time from the origin's known position.
func BuildRxEvent(pkt *aprs.DecodedAPRSPacket) (RxEvent, bool) {
	if pkt == nil || pkt.ThirdParty != nil || pkt.Source == "" {
		return RxEvent{}, false
	}
	ts := pkt.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	hops := aprs.CountHops(pkt.Path)
	ev := RxEvent{Timestamp: ts, Hops: hops}
	if digi := lastHBitDigi(pkt.Path); hops > 0 && digi != "" {
		ev.AttrKey = "stn:" + digi
	} else {
		ev.AttrKey = "stn:" + pkt.Source
		if pkt.Position != nil {
			ev.Lat = pkt.Position.Latitude
			ev.Lon = pkt.Position.Longitude
			ev.HasPos = true
		}
	}
	return ev, true
}
