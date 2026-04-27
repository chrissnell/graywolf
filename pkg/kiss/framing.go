// Package kiss implements the KISS protocol (TNC framing + TCP/serial
// transports) as specified in Mike Chepponis / Phil Karn's KISS document.
//
// A KISS frame on the wire is:
//
//	FEND <type> <data...> FEND
//
// where the type byte's low nibble is the command (0=data, 1=txdelay, ...)
// and the high nibble is the port number (0..15). Escaping rules:
//
//	FEND  (0xC0) -> FESC TFEND (0xDB 0xDC)
//	FESC  (0xDB) -> FESC TFESC (0xDB 0xDD)
package kiss

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// KISS wire constants.
const (
	FEND  byte = 0xC0
	FESC  byte = 0xDB
	TFEND byte = 0xDC
	TFESC byte = 0xDD
)

// KISS command codes (low nibble of type byte).
const (
	CmdDataFrame    byte = 0x00
	CmdTxDelay      byte = 0x01
	CmdPersistence  byte = 0x02
	CmdSlotTime     byte = 0x03
	CmdTxTail       byte = 0x04
	CmdFullDuplex   byte = 0x05
	CmdSetHardware  byte = 0x06
	CmdReturn       byte = 0xFF
)

// ErrInvalidEscape is returned when a frame contains FESC followed by an
// invalid escape byte.
var ErrInvalidEscape = errors.New("kiss: invalid escape sequence")

// A Frame is one decoded KISS frame.
type Frame struct {
	Port    uint8  // 0..15
	Command byte   // low nibble of the type byte
	Data    []byte // unescaped payload
}

// Encode returns the wire bytes for a KISS data frame (command=0x00) on
// the given port, with proper escaping. Zero-copy is not possible because
// of the escapes; the returned slice is a fresh allocation.
func Encode(port uint8, data []byte) []byte {
	return EncodeCommand(port, CmdDataFrame, data)
}

// EncodeCommand is like Encode but for any command byte (txdelay etc).
func EncodeCommand(port uint8, cmd byte, data []byte) []byte {
	out := make([]byte, 0, len(data)+4)
	out = append(out, FEND)
	out = append(out, (port<<4)|(cmd&0x0F))
	for _, b := range data {
		switch b {
		case FEND:
			out = append(out, FESC, TFEND)
		case FESC:
			out = append(out, FESC, TFESC)
		default:
			out = append(out, b)
		}
	}
	out = append(out, FEND)
	return out
}

// A Decoder consumes a stream of KISS-framed bytes and yields Frames. It
// handles leading/trailing FENDs and resynchronises on a stream error by
// discarding until the next FEND.
type Decoder struct {
	br *bufio.Reader
	// Max payload size; frames larger than this return an error.
	MaxFrame int
}

// NewDecoder wraps r in a buffered KISS decoder. MaxFrame defaults to 4096.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{br: bufio.NewReader(r), MaxFrame: 4096}
}

// Next reads the next complete frame. Returns io.EOF when the underlying
// reader is exhausted cleanly.
func (d *Decoder) Next() (*Frame, error) {
	// Skip leading FENDs and any stray bytes until we see a FEND.
	for {
		b, err := d.br.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == FEND {
			break
		}
	}
	// Now read until next FEND, unescaping as we go.
	buf := make([]byte, 0, 64)
	for {
		b, err := d.br.ReadByte()
		if err != nil {
			return nil, err
		}
		switch b {
		case FEND:
			// Empty frame (two FENDs in a row) — skip and loop for a real one.
			if len(buf) == 0 {
				continue
			}
			if d.MaxFrame > 0 && len(buf) > d.MaxFrame {
				return nil, fmt.Errorf("kiss: frame too large: %d > %d", len(buf), d.MaxFrame)
			}
			typeByte := buf[0]
			return &Frame{
				Port:    typeByte >> 4,
				Command: typeByte & 0x0F,
				Data:    append([]byte(nil), buf[1:]...),
			}, nil
		case FESC:
			nb, err := d.br.ReadByte()
			if err != nil {
				return nil, err
			}
			switch nb {
			case TFEND:
				buf = append(buf, FEND)
			case TFESC:
				buf = append(buf, FESC)
			default:
				return nil, ErrInvalidEscape
			}
		default:
			buf = append(buf, b)
		}
		if d.MaxFrame > 0 && len(buf) > d.MaxFrame+1 {
			return nil, fmt.Errorf("kiss: frame too large: %d > %d", len(buf), d.MaxFrame)
		}
	}
}
