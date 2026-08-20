package weather

import (
	"strings"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// AlertStatusWarning and AlertStatusWatch are the two active alert statuses.
// AlertStatusClear is the default when no active alert exists for a county.
const (
	AlertStatusClear   = "clear"
	AlertStatusWatch   = "watch"
	AlertStatusWarning = "warning"
)

// NWSAlert holds the parsed contents of one NWS APRS alert message.
type NWSAlert struct {
	// Addressee is the original message addressee (e.g. "NWS-WARN").
	Addressee string
	// AlertStatus is "warning" or "watch", derived from the addressee.
	AlertStatus string
	// AlertType is the event type string from the message body (e.g. "FLASH_FLOOD").
	AlertType string
	// NWSCodes is the list of NWS county zone codes from the message body
	// (e.g. ["OHC019", "OHC029"]).
	NWSCodes []string
}

// NWSPositionEvent holds the parsed contents of an NWS position object.
type NWSPositionEvent struct {
	// WFO is the 3-letter Weather Forecast Office identifier derived from the
	// source callsign (e.g. "PBZ" from source "PBZSVR").
	WFO string
	// AlertType is the event type keyword found in the object comment.
	AlertType string
	// AlertStatus is "warning" or "watch", derived from the alert type.
	AlertStatus string
}

// ParseNWSMessage extracts alert details from an NWS APRS message packet.
// Returns (alert, true) when the packet is a parseable NWS message, or
// (nil, false) for non-NWS packets or messages with no county codes.
func ParseNWSMessage(pkt *aprs.DecodedAPRSPacket) (*NWSAlert, bool) {
	if pkt == nil || pkt.Message == nil || !pkt.Message.IsNWS {
		return nil, false
	}
	addr := strings.TrimSpace(pkt.Message.Addressee)
	status := AlertStatusFromAddressee(addr)

	// Message body format: "DDHHMMz,TYPE,NWS1,NWS2,...{msgid"
	body := pkt.Message.Text
	// Strip trailing ACK/message-ID suffix starting at '{'.
	if idx := strings.IndexByte(body, '{'); idx >= 0 {
		body = body[:idx]
	}
	body = strings.TrimSpace(body)
	parts := strings.Split(body, ",")
	if len(parts) < 3 {
		return nil, false
	}
	// parts[0] is the timestamp (DDHHMMz), parts[1] is the alert type,
	// parts[2..] are the county NWS codes.
	alertType := strings.TrimSpace(parts[1])
	if alertType == "" {
		return nil, false
	}
	var codes []string
	for _, p := range parts[2:] {
		if c := strings.TrimSpace(p); c != "" {
			codes = append(codes, strings.ToUpper(c))
		}
	}
	if len(codes) == 0 {
		return nil, false
	}
	if status == AlertStatusClear {
		status = AlertStatusFromType(alertType)
	}
	return &NWSAlert{
		Addressee:   addr,
		AlertStatus: status,
		AlertType:   alertType,
		NWSCodes:    codes,
	}, true
}

// ParseNWSPosition extracts WFO and alert type from an NWS position object
// packet. Returns (event, true) when the packet looks like an NWS position
// object, or (nil, false) otherwise.
func ParseNWSPosition(pkt *aprs.DecodedAPRSPacket) (*NWSPositionEvent, bool) {
	if pkt == nil {
		return nil, false
	}
	// NWS position objects may arrive as PacketObject or PacketPosition.
	src := strings.ToUpper(strings.TrimSpace(pkt.Source))
	if !IsNWSWFOCallsign(src) {
		return nil, false
	}
	// Extract the alert type from the comment; look for known keywords.
	comment := ""
	switch {
	case pkt.Object != nil:
		comment = pkt.Object.Comment
	case pkt.Position != nil:
		comment = pkt.Comment
	default:
		return nil, false
	}
	alertType := alertTypeFromComment(comment)
	if alertType == "" {
		return nil, false
	}
	wfo := src[:3]
	return &NWSPositionEvent{
		WFO:         wfo,
		AlertType:   alertType,
		AlertStatus: AlertStatusFromType(alertType),
	}, true
}

// AlertStatusFromAddressee maps an NWS APRS message addressee to an alert
// status. Uses hyphen-separated suffixes only (NWS-WARN, NWS-WTCH).
func AlertStatusFromAddressee(addr string) string {
	addr = strings.ToUpper(addr)
	switch {
	case strings.HasSuffix(addr, "-WARN"):
		return AlertStatusWarning
	case strings.HasSuffix(addr, "-WTCH"):
		return AlertStatusWatch
	default:
		return AlertStatusClear
	}
}

// AlertStatusFromType infers watch vs warning from the alert type string.
// Types that end in _WTCH are watches; everything else is treated as a warning.
func AlertStatusFromType(alertType string) string {
	if strings.HasSuffix(strings.ToUpper(alertType), "_WTCH") {
		return AlertStatusWatch
	}
	return AlertStatusWarning
}

// alertTypeFromComment searches the object comment for a known NWS alert
// keyword and returns the first match, or "" when none is found.
func alertTypeFromComment(comment string) string {
	upper := strings.ToUpper(comment)
	for kw := range NWSAlertKeywords {
		if strings.Contains(upper, kw) {
			return kw
		}
	}
	return ""
}
