package aprs

import (
	"strconv"
	"strings"
)

// DFReportPrefix is the APRS direction-finding signalling prefix that
// may appear as the symbol code ('\' table + '#' code) or inline as
// "/BRG/NRQ" appended to a position comment.
const DFReportPrefix = "DFS"

// parseDirectionFinding scans a trailing "/BRG/NRQ" appendix on the
// comment of an already-decoded position packet. Call from higher
// layers after Parse when pkt.Position != nil and pkt.Comment may hold
// a DF signature. Returns true if a DF field was consumed. APRS101 ch 7
// specifies DF range as 2^R miles.
func parseDirectionFinding(pkt *DecodedAPRSPacket) bool {
	if pkt == nil || pkt.Position == nil {
		return false
	}
	c := pkt.Comment
	// Canonical form: "/BBB/NRQ" at any position in the comment.
	idx := strings.Index(c, "/")
	for idx >= 0 {
		if idx+8 <= len(c) && c[idx+4] == '/' {
			brg := c[idx+1 : idx+4]
			nrq := c[idx+5 : idx+8]
			if isDigit3(brg) && isDigit3(nrq) {
				b, _ := strconv.Atoi(brg)
				n, _ := strconv.Atoi(string(nrq[0]))
				r, _ := strconv.Atoi(string(nrq[1]))
				q, _ := strconv.Atoi(string(nrq[2]))
				pkt.DF = &DirectionFinding{
					Bearing: b,
					Number:  n,
					Range:   1 << r, // 2^r miles per APRS spec
					Quality: q,
				}
				// Strip the DF block from the comment for cleanliness.
				pkt.Comment = strings.TrimSpace(c[:idx] + c[idx+8:])
				return true
			}
		}
		next := strings.Index(c[idx+1:], "/")
		if next < 0 {
			break
		}
		idx = idx + 1 + next
	}
	return false
}
