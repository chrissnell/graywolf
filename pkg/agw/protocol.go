// Package agw implements a minimal AGWPE-compatible TCP server for APRS
// use. The wire format matches direwolf's server.c so existing clients
// (APRSIS32, UI-View, YAAC, Xastir) interoperate without modification.
//
// Only the frame types needed for APRS are implemented:
//
//   Client → server:
//     'R'  query AGW version
//     'G'  query port information (list of radio ports)
//     'g'  query port capabilities
//     'X'  register callsign
//     'x'  unregister callsign
//     'm'  start monitoring (enable 'U'-type rx packets)
//     'k'  transmit raw AX.25 frame
//     'M'  transmit UNPROTO (UI) frame — server must build the AX.25 header
//
//   Server → client:
//     'R'  version response
//     'G'  port info response
//     'g'  port capability response
//     'X'  callsign registered ack
//     'U'  monitored UI frame from RF
//
// Connected-mode frame types ('C', 'D', 'd', 'v', 'V', 'c', ...) are
// accepted and logged but not implemented, matching the "AX.25 UI only"
// constraint for graywolf Phase 2.
package agw

import (
	"encoding/binary"
	"errors"
	"io"
)

// HeaderSize is the fixed AGW frame header length.
const HeaderSize = 36

// Header is the 36-byte AGWPE frame header.
type Header struct {
	Port     uint8
	DataKind byte
	PID      uint8
	CallFrom string // 10-char NUL-padded
	CallTo   string // 10-char NUL-padded
	DataLen  uint32 // little-endian on the wire
	User     uint32
}

// Data kinds as single ASCII bytes. Direction is context-dependent (some
// kinds appear in both directions).
const (
	KindVersion            byte = 'R'
	KindPortInfo           byte = 'G'
	KindPortCaps           byte = 'g'
	KindRegisterCallsign   byte = 'X'
	KindUnregisterCallsign byte = 'x'
	KindMonitorOn          byte = 'm'
	KindSendUnproto        byte = 'M' // client → server: send UI frame
	KindSendUnprotoVia     byte = 'V' // client → server: send UI frame via digipeaters
	KindSendRaw            byte = 'K' // both directions: raw AX.25
	KindMonitoredUI        byte = 'U' // server → client: rx UI frame
)

// EncodeHeader writes h into a 36-byte buffer.
func EncodeHeader(h *Header) []byte {
	buf := make([]byte, HeaderSize)
	buf[0] = h.Port
	buf[4] = h.DataKind
	buf[6] = h.PID
	copyPadded(buf[8:18], h.CallFrom)
	copyPadded(buf[18:28], h.CallTo)
	binary.LittleEndian.PutUint32(buf[28:32], h.DataLen)
	binary.LittleEndian.PutUint32(buf[32:36], h.User)
	return buf
}

// DecodeHeader parses a 36-byte header.
func DecodeHeader(buf []byte) (*Header, error) {
	if len(buf) < HeaderSize {
		return nil, errors.New("agw: short header")
	}
	h := &Header{
		Port:     buf[0],
		DataKind: buf[4],
		PID:      buf[6],
		CallFrom: trimNul(string(buf[8:18])),
		CallTo:   trimNul(string(buf[18:28])),
		DataLen:  binary.LittleEndian.Uint32(buf[28:32]),
		User:     binary.LittleEndian.Uint32(buf[32:36]),
	}
	return h, nil
}

// ReadFrame reads one header + data payload from r.
func ReadFrame(r io.Reader) (*Header, []byte, error) {
	hbuf := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, hbuf); err != nil {
		return nil, nil, err
	}
	h, err := DecodeHeader(hbuf)
	if err != nil {
		return nil, nil, err
	}
	const maxDataLen = 1 << 16
	if h.DataLen > maxDataLen {
		return nil, nil, errors.New("agw: data_len too large")
	}
	var data []byte
	if h.DataLen > 0 {
		data = make([]byte, h.DataLen)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, nil, err
		}
	}
	return h, data, nil
}

// WriteFrame serialises h (with DataLen overwritten to len(data)) and
// writes header+data to w atomically using a single buffer allocation.
func WriteFrame(w io.Writer, h *Header, data []byte) error {
	h.DataLen = uint32(len(data))
	hbuf := EncodeHeader(h)
	buf := make([]byte, 0, HeaderSize+len(data))
	buf = append(buf, hbuf...)
	buf = append(buf, data...)
	_, err := w.Write(buf)
	return err
}

func copyPadded(dst []byte, s string) {
	n := copy(dst, s)
	for i := n; i < len(dst); i++ {
		dst[i] = 0
	}
}

func trimNul(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return s[:i]
		}
	}
	return s
}
