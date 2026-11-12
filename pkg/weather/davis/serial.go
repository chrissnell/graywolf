package davis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/chrissnell/graywolf/pkg/weather"
	"go.bug.st/serial"
)

const DefaultBaud = 19200

var (
	wakeTimeout   = 1200 * time.Millisecond
	ackTimeout    = time.Second
	packetTimeout = 5 * time.Second
)

type timeoutPort interface {
	io.ReadWriteCloser
	SetReadTimeout(time.Duration) error
}

type SerialConfig struct {
	Device string
	Baud   int
	Bucket Bucket
}

var _ weather.Source = SerialConfig{}

// Read lets SerialConfig satisfy weather.Source.
func (cfg SerialConfig) Read(ctx context.Context) (weather.Observation, error) {
	return ReadSerial(ctx, cfg)
}

// ReadSerial opens a Davis console, wakes it, and requests one LOOP plus one
// LOOP2 packet. Asking for two packets preserves the alternating merge used by
// a long LPS 3 stream while avoiding a ten-minute burst for a periodic beacon.
func ReadSerial(ctx context.Context, cfg SerialConfig) (weather.Observation, error) {
	if cfg.Device == "" {
		return weather.Observation{}, fmt.Errorf("davis: serial device is required")
	}
	baud := cfg.Baud
	if baud == 0 {
		baud = DefaultBaud
	}
	port, err := serial.Open(cfg.Device, &serial.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return weather.Observation{}, fmt.Errorf("davis: open %s: %w", cfg.Device, err)
	}
	return Exchange(ctx, port, cfg.Bucket)
}

// Exchange performs the Davis conversation on an already-open port. It is
// exported so the stateful serial protocol can be replayed in tests.
func Exchange(ctx context.Context, port timeoutPort, bucket Bucket) (weather.Observation, error) {
	defer port.Close()
	if bucket == "" {
		bucket = Bucket02MM
	}
	woken := false
	for attempt := 0; attempt < 3 && !woken; attempt++ {
		if err := writeAll(ctx, port, []byte{'\n'}); err != nil {
			return weather.Observation{}, fmt.Errorf("davis: wake: %w", err)
		}
		if err := port.SetReadTimeout(wakeTimeout); err != nil {
			return weather.Observation{}, fmt.Errorf("davis: set wake timeout: %w", err)
		}
		woken = readSequence(ctx, port, []byte{'\n', '\r'}) == nil
	}
	if !woken {
		return weather.Observation{}, fmt.Errorf("davis: console did not answer wakeup after 3 attempts")
	}
	if err := writeAll(ctx, port, []byte("LPS 3 2\n")); err != nil {
		return weather.Observation{}, fmt.Errorf("davis: send LOOP command: %w", err)
	}
	if err := port.SetReadTimeout(ackTimeout); err != nil {
		return weather.Observation{}, fmt.Errorf("davis: set ACK timeout: %w", err)
	}
	if err := readSequence(ctx, port, []byte{0x06}); err != nil {
		return weather.Observation{}, fmt.Errorf("davis: ACK: %w", err)
	}

	var observation weather.Observation
	seen := map[PacketType]bool{}
	for i := 0; i < 2; i++ {
		if err := port.SetReadTimeout(packetTimeout); err != nil {
			return weather.Observation{}, fmt.Errorf("davis: set packet timeout: %w", err)
		}
		packet := make([]byte, PacketLength)
		if err := readFull(ctx, port, packet); err != nil {
			return weather.Observation{}, fmt.Errorf("davis: packet %d: %w", i+1, err)
		}
		typ, err := DecodeLOOP(packet, bucket, &observation)
		if err != nil {
			return weather.Observation{}, err
		}
		seen[typ] = true
	}
	if !seen[PacketLOOP] || !seen[PacketLOOP2] {
		return weather.Observation{}, fmt.Errorf("davis: expected one LOOP and one LOOP2 packet")
	}
	return observation, nil
}

func writeAll(ctx context.Context, w io.Writer, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func readSequence(ctx context.Context, r io.Reader, want []byte) error {
	matched := 0
	buf := []byte{0}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.Read(buf)
		if n > 0 {
			if len(want) == 1 && want[0] == 0x06 && buf[0] == 0x21 {
				return errors.New("console returned NAK")
			}
			if buf[0] == want[matched] {
				matched++
				if matched == len(want) {
					return nil
				}
			} else if buf[0] == want[0] {
				matched = 1
			} else {
				matched = 0
			}
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("read timeout")
		}
	}
}

func readFull(ctx context.Context, r io.Reader, data []byte) error {
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.Read(data[offset:])
		offset += n
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("read timeout")
		}
	}
	return nil
}
