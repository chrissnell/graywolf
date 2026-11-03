package tncprofile

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrKenwoodCommandMode makes the serial supervisor reopen and initialize a
// radio that rebooted while the independently powered USB adapter stayed open.
// Kenwood monitor text after a reboot has a TNC2 header outside KISS.
var kenwoodMonitorHeader = regexp.MustCompile(`^[A-Z0-9]{1,6}(?:-[0-9]{1,2})?>[A-Z0-9]{1,6}(?:-[0-9]{1,2})?(?:,[A-Z0-9]{1,6}(?:-[0-9]{1,2})?\*?)*:$`)

var ErrKenwoodCommandMode = errors.New("Kenwood returned to command mode; reinitialization required")

type kenwoodSession struct {
	mu     sync.Mutex
	echoes []commandEcho
	rxTail []byte
	io.ReadWriteCloser
	skip      int
	frame     bool
	boundary  bool
	candidate bool
	line      string
}

func newKenwoodSession(port io.ReadWriteCloser, startupBytes int) *kenwoodSession {
	return &kenwoodSession{ReadWriteCloser: port, skip: startupBytes}
}

type commandEcho struct {
	bytes []byte
	until time.Time
}

// A command-mode TNC may echo a submitted KISS frame while consuming its
// AX.25 Ctrl-C (0x03). This exact altered echo was observed after power-on.
// Match only our own recent writes, never generic invalid RF frames.
func (p *kenwoodSession) Write(data []byte) (int, error) {
	if len(data) >= 19 && len(data) <= 4096 && data[0] == 0xc0 && data[1]&15 == 0 && data[len(data)-1] == 0xc0 {
		offset := 2
		for offset+7 < len(data) {
			last := data[offset+6]&1 != 0
			offset += 7
			if last {
				if offset+1 < len(data) && data[offset] == 3 && data[offset+1] == 0xf0 {
					echo := append([]byte(nil), data[:offset]...)
					echo = append(echo, data[offset+1:]...)
					p.mu.Lock()
					p.echoes = append(p.echoes, commandEcho{echo, time.Now().Add(30 * time.Second)})
					if len(p.echoes) > 4 {
						p.echoes = p.echoes[len(p.echoes)-4:]
					}
					p.mu.Unlock()
				}
				break
			}
		}
	}
	return p.ReadWriteCloser.Write(data)
}

func (p *kenwoodSession) commandEcho(data []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rxTail = append(p.rxTail, data...)
	if len(p.rxTail) > 8192 {
		p.rxTail = p.rxTail[len(p.rxTail)-8192:]
	}
	now := time.Now()
	for _, echo := range p.echoes {
		if now.Before(echo.until) && bytes.Contains(p.rxTail, echo.bytes) {
			return true
		}
	}
	return false
}

func (p *kenwoodSession) Read(dst []byte) (int, error) {
	n, err := p.ReadWriteCloser.Read(dst)
	if p.commandEcho(dst[:n]) {
		_ = p.ReadWriteCloser.Close()
		return 0, ErrKenwoodCommandMode
	}
	for _, b := range dst[:n] {
		startup := p.skip > 0
		if startup {
			p.skip--
		}
		if b == 0xc0 {
			p.frame = false
			p.boundary = true
			p.candidate = false
			p.line = ""
			continue
		}
		// Only a delimiter can start a KISS frame. Printable data-command
		// bytes (e.g. P for port 5) are ambiguous with the radio's PS 1
		// startup report. Confirm their binary AX.25 header before masking
		// text; every valid UI header has a binary control byte before APRS.
		if p.boundary {
			p.boundary = false
			if b == 0xdb { // escaped KISS command byte (port 12 data)
				p.frame = true
			}
			if b&0x0f == 0 {
				if b < 32 || b >= 127 {
					p.frame = true
				} else {
					p.candidate = true
				}
			}
		}
		if p.candidate {
			if b == '\r' || b == '\n' {
				p.candidate = false
			} else if b < 32 || b >= 127 {
				p.frame = true
				p.candidate = false
			}
		}
		if p.frame {
			continue
		}
		if b == '\r' || b == '\n' {
			p.line = ""
			continue
		}
		if b < 32 || b >= 127 {
			p.line = ""
			continue
		}
		p.line += string(b)
		if len(p.line) > 128 {
			p.line = p.line[len(p.line)-128:]
		}
		if !startup && (strings.HasSuffix(strings.ToLower(p.line), "cmd:") || strings.Contains(p.line, "Kenwood Radio Modem") || (b == ':' && kenwoodMonitorHeader.MatchString(p.line))) {
			_ = p.ReadWriteCloser.Close()
			return 0, ErrKenwoodCommandMode
		}
	}
	return n, err
}
