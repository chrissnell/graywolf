package aprs

import (
	"errors"
	"strconv"
	"strings"
)

// parseMessage handles the ':' prefix: addressee (9 chars) + ':' + text
// with optional {id ack/rej trailer.
//
// Wire format: ":AAAAAAAAA:text{id"  (addressee is exactly 9 chars, padded)
func parseMessage(pkt *DecodedAPRSPacket, info []byte) error {
	if len(info) < 11 || info[10] != ':' {
		return errors.New("aprs: malformed message header")
	}
	addressee := strings.TrimRight(string(info[1:10]), " ")
	rest := string(info[11:])
	msg := &Message{Addressee: addressee}
	// Trailing {id separator for message IDs. Reply-ack form
	// (aprs.org/aprs11/replyacks.txt): "text{id}" (empty piggybacked
	// ack) or "text{id}ackid" (ackid being acked on this outgoing
	// message). Detect either form before fall-through id extraction.
	if brace := strings.LastIndexByte(rest, '{'); brace >= 0 {
		tail := rest[brace+1:]
		if closeIdx := strings.IndexByte(tail, '}'); closeIdx >= 0 {
			// Reply-ack trailer present.
			msg.MessageID = tail[:closeIdx]
			msg.ReplyAck = tail[closeIdx+1:]
			msg.HasReplyAck = true
			rest = rest[:brace]
		} else {
			msg.MessageID = tail
			rest = rest[:brace]
		}
	}
	// ack123 / rej123 replies.
	switch {
	case strings.HasPrefix(rest, "ack"):
		msg.IsAck = true
		msg.MessageID = rest[3:]
		rest = ""
	case strings.HasPrefix(rest, "rej"):
		msg.IsRej = true
		msg.MessageID = rest[3:]
		rest = ""
	}
	msg.Text = rest
	if strings.HasPrefix(addressee, "BLN") {
		msg.IsBulletin = true
	}
	if strings.HasPrefix(addressee, "NWS") || strings.HasPrefix(addressee, "SKY") ||
		strings.HasPrefix(addressee, "CWA") {
		msg.IsNWS = true
	}
	pkt.Message = msg
	pkt.Type = PacketMessage
	// APRS101 ch 13: PARM./UNIT./EQNS./BITS. messages are addressed to
	// the telemetering station itself and carry channel metadata.
	if meta := parseTelemetryMeta(rest); meta != nil {
		pkt.TelemetryMeta = meta
		pkt.Type = PacketTelemetry
	}
	return nil
}

// parseTelemetryMeta recognizes the four telemetry metadata message
// bodies (APRS101 ch 13.1). Returns nil if text does not start with a
// recognized prefix.
func parseTelemetryMeta(text string) *TelemetryMeta {
	switch {
	case strings.HasPrefix(text, "PARM."):
		m := &TelemetryMeta{Kind: "parm"}
		fields := strings.Split(text[len("PARM."):], ",")
		for i := 0; i < len(fields) && i < len(m.Parm); i++ {
			m.Parm[i] = strings.TrimSpace(fields[i])
		}
		return m
	case strings.HasPrefix(text, "UNIT."):
		m := &TelemetryMeta{Kind: "unit"}
		fields := strings.Split(text[len("UNIT."):], ",")
		for i := 0; i < len(fields) && i < len(m.Unit); i++ {
			m.Unit[i] = strings.TrimSpace(fields[i])
		}
		return m
	case strings.HasPrefix(text, "EQNS."):
		m := &TelemetryMeta{Kind: "eqns"}
		fields := strings.Split(text[len("EQNS."):], ",")
		for ch := 0; ch < 5; ch++ {
			for k := 0; k < 3; k++ {
				idx := ch*3 + k
				if idx >= len(fields) {
					break
				}
				v, _ := strconv.ParseFloat(strings.TrimSpace(fields[idx]), 64)
				m.Eqns[ch][k] = v
			}
		}
		return m
	case strings.HasPrefix(text, "BITS."):
		m := &TelemetryMeta{Kind: "bits"}
		body := text[len("BITS."):]
		// Format: "11111111,Project Name"
		if comma := strings.IndexByte(body, ','); comma >= 0 {
			m.ProjectName = body[comma+1:]
			body = body[:comma]
		}
		var bits uint8
		for i := 0; i < len(body) && i < 8; i++ {
			if body[i] == '1' {
				bits |= 1 << (7 - i)
			}
		}
		m.Bits = bits
		return m
	}
	return nil
}

// EncodeMessage builds the info field for an APRS message. The result
// includes the leading ':' type indicator; callers concatenate it with
// the AX.25 header via ax25.NewUIFrame.
func EncodeMessage(addressee, text, id string) ([]byte, error) {
	if len(addressee) == 0 || len(addressee) > 9 {
		return nil, errors.New("aprs: addressee length 1..9")
	}
	out := make([]byte, 0, 11+len(text)+len(id)+2)
	out = append(out, ':')
	padded := addressee + strings.Repeat(" ", 9-len(addressee))
	out = append(out, padded...)
	out = append(out, ':')
	out = append(out, text...)
	if id != "" {
		out = append(out, '{')
		out = append(out, id...)
	}
	return out, nil
}

// EncodeMessageAck builds an "ack{id}" reply targeted at the original
// sender.
func EncodeMessageAck(addressee, id string) ([]byte, error) {
	if id == "" {
		return nil, errors.New("aprs: empty message id")
	}
	return EncodeMessage(addressee, "ack"+id, "")
}
