package weather

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"go.bug.st/serial"
)

const PeetDefaultBaud = 2400

var peetReadTimeout = 15 * time.Second

// PeetRainUnit describes the rain-counter unit selected in the Ultimeter.
// Most configurations report hundredths of an inch; consoles configured for
// the metric 0.1 mm collector instead report tenths of a millimetre.
type PeetRainUnit string

const (
	PeetRain001In PeetRainUnit = "0.01in"
	PeetRain01MM  PeetRainUnit = "0.1mm"
)

type PeetSerialConfig struct {
	Device   string
	Baud     int
	RainUnit PeetRainUnit
}

var _ Source = PeetSerialConfig{}

type peetSerialPort interface {
	io.ReadCloser
	SetReadTimeout(time.Duration) error
}

func (cfg PeetSerialConfig) Read(ctx context.Context) (Observation, error) {
	if strings.TrimSpace(cfg.Device) == "" {
		return Observation{}, errors.New("peet: serial device is required")
	}
	baud := cfg.Baud
	if baud == 0 {
		baud = PeetDefaultBaud
	}
	port, err := serial.Open(cfg.Device, &serial.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return Observation{}, fmt.Errorf("peet: open %s: %w", cfg.Device, err)
	}
	return ReadPeetStream(ctx, port, cfg.RainUnit)
}

// ReadPeetStream returns the first complete Data Logger (!!) or Packet Mode
// ($ULTW) record from an already-open serial stream. Peet consoles continuously
// emit records; no command/response exchange is required.
func ReadPeetStream(ctx context.Context, port peetSerialPort, rainUnit PeetRainUnit) (Observation, error) {
	defer port.Close()
	if err := port.SetReadTimeout(peetReadTimeout); err != nil {
		return Observation{}, fmt.Errorf("peet: set read timeout: %w", err)
	}

	var line []byte
	var lastParseErr error
	buf := make([]byte, 256)
	for {
		if err := ctx.Err(); err != nil {
			return Observation{}, err
		}
		n, err := port.Read(buf)
		for _, c := range buf[:n] {
			if c == '\r' || c == '\n' {
				if len(line) != 0 {
					observation, parseErr := ParsePeetLine(string(line), rainUnit)
					if parseErr == nil {
						return observation, nil
					}
					lastParseErr = parseErr
					line = line[:0]
				}
				continue
			}
			if len(line) < 1024 {
				line = append(line, c)
			} else {
				line = line[:0]
				lastParseErr = errors.New("peet: serial record exceeds 1024 bytes")
			}
		}
		if err != nil {
			if lastParseErr != nil {
				return Observation{}, lastParseErr
			}
			return Observation{}, fmt.Errorf("peet: read serial record: %w", err)
		}
		if n == 0 {
			if lastParseErr != nil {
				return Observation{}, lastParseErr
			}
			return Observation{}, errors.New("peet: serial record timeout")
		}
	}
}

// ParsePeetLine converts one Peet Bros Data Logger or Packet Mode record into
// the hardware-independent observation used by all Graywolf weather sources.
func ParsePeetLine(line string, rainUnit PeetRainUnit) (Observation, error) {
	line = strings.TrimSpace(line)
	var payload string
	var allowed map[int]bool
	switch {
	case strings.HasPrefix(line, "$ULTW"):
		payload = line[5:]
		allowed = map[int]bool{44: true, 48: true, 52: true}
	case strings.HasPrefix(line, "!!"):
		payload = line[2:]
		allowed = map[int]bool{40: true, 44: true, 48: true}
	default:
		return Observation{}, errors.New("peet: record must start with $ULTW or !!")
	}
	if !allowed[len(payload)] {
		return Observation{}, fmt.Errorf("peet: unexpected payload length %d", len(payload))
	}
	for offset := 0; offset < len(payload); offset += 4 {
		field := payload[offset : offset+4]
		if field == "----" {
			continue
		}
		for _, c := range []byte(field) {
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')) {
				return Observation{}, fmt.Errorf("peet: invalid hex field %q", field)
			}
		}
	}

	decoded, err := aprs.ParseInfo([]byte(line))
	if err != nil {
		return Observation{}, fmt.Errorf("peet: decode record: %w", err)
	}
	if decoded.Type != aprs.PacketWeather || decoded.Weather == nil {
		return Observation{}, errors.New("peet: record did not decode as weather")
	}
	observation := fromAPRS(decoded.Weather)

	switch rainUnit {
	case "", PeetRain001In:
		// fromAPRS already interprets the raw counter as 0.01 inch.
	case PeetRain01MM:
		if observation.RainDayMM != nil {
			// Undo the APRS parser's 0.01-inch interpretation and apply the
			// metric Ultimeter collector's 0.1-mm counter unit.
			raw := *observation.RainDayMM / mmPerInch * 100
			*observation.RainDayMM = raw * 0.1
		}
	default:
		return Observation{}, fmt.Errorf("peet: unsupported rain unit %q", rainUnit)
	}
	return observation, nil
}
