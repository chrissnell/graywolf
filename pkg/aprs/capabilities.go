package aprs

import "strings"

// parseCapabilities decodes a station-capabilities packet ("<IGATE,MSG_CNT=12,LOC_CNT=34>").
// The leading '<' has already been matched by the dispatcher. Entries
// are comma-separated, each either "KEY" or "KEY=VALUE".
func parseCapabilities(pkt *DecodedAPRSPacket, info []byte) error {
	body := string(info[1:])
	body = strings.TrimSuffix(body, ">")
	caps := &Capabilities{Entries: make(map[string]string)}
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if eq := strings.IndexByte(part, '='); eq >= 0 {
			caps.Entries[part[:eq]] = part[eq+1:]
		} else {
			caps.Entries[part] = ""
		}
	}
	pkt.Caps = caps
	pkt.Type = PacketCapabilities
	return nil
}
