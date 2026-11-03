// Package tncprofile initializes command-mode TNCs before handing their
// serial stream to the KISS transport. Native KISS devices remain untouched.
package tncprofile

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	Native  = ""
	Kenwood = "kenwood"
	TMD700  = "kenwood-tm-d700"
	TMD710  = "kenwood-tm-d710"
)

func Valid(profile string) bool {
	return profile == Native || profile == Kenwood || profile == TMD700 || profile == TMD710
}

// Initialize owns the stream until initialization completes. A context
// timeout closes the port to interrupt blocking reads/writes. The returned
// wrapper preserves bytes already buffered after the command prompt.
func Initialize(ctx context.Context, port io.ReadWriteCloser, profile string) (io.ReadWriteCloser, error) {
	return InitializeWithRadioBaud(ctx, port, profile, 0)
}

// InitializeWithRadioBaud optionally sets the RF packet speed, independently
// from the computer's serial baud rate. Zero leaves the radio's speed alone.
func InitializeWithRadioBaud(ctx context.Context, port io.ReadWriteCloser, profile string, radioBaud uint32) (io.ReadWriteCloser, error) {
	if profile == TMD700 || profile == TMD710 {
		return initializeConfigured(ctx, port, profile, radioBaud, 4*time.Second, 5*time.Second)
	}
	return initializeConfigured(ctx, port, profile, radioBaud, 0, 1500*time.Millisecond)
}

func initialize(parent context.Context, port io.ReadWriteCloser, profile string, settle time.Duration) (io.ReadWriteCloser, error) {
	return initializeConfigured(parent, port, profile, 0, 0, settle)
}

func ValidateRadioBaud(profile string, rate uint32) error {
	if rate == 0 {
		return nil
	}
	if rate != 1200 && rate != 9600 {
		return fmt.Errorf("radio_baud_rate must be 0 (preserve), 1200 or 9600")
	}
	if profile != TMD700 && profile != TMD710 {
		return fmt.Errorf("radio_baud_rate requires a TM-D700 or TM-D710 model profile")
	}
	return nil
}

func initializeConfigured(parent context.Context, port io.ReadWriteCloser, profile string, radioBaud uint32, modeSettle, settle time.Duration) (io.ReadWriteCloser, error) {
	if err := ValidateRadioBaud(profile, radioBaud); err != nil {
		return nil, err
	}
	if profile == Native {
		return port, nil
	}
	if !Valid(profile) {
		return nil, fmt.Errorf("unknown TNC initialization profile %q", profile)
	}
	if profile == TMD700 || profile == TMD710 {
		return initializeModel(parent, port, profile, radioBaud, modeSettle, settle)
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	stopClose := context.AfterFunc(ctx, func() { _ = port.Close() })
	defer stopClose()
	reader := bufio.NewReader(port)
	write := func(data string) error {
		n, err := io.WriteString(port, data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		return err
	}
	prompt := func() error {
		var tail string
		for i := 0; i < 8192; i++ {
			b, err := reader.ReadByte()
			if err != nil {
				return err
			}
			tail += string(b)
			if len(tail) > 4 {
				tail = tail[len(tail)-4:]
			}
			if strings.EqualFold(tail, "cmd:") {
				return nil
			}
		}
		return fmt.Errorf("command prompt not found within 8192 bytes")
	}
	// The binary KISS return handles a radio already in KISS after reconnect.
	// Ctrl-C leaves converse mode before sending any CR; the second Ctrl-C
	// clears text left at the command prompt by the binary return sequence.
	steps := []struct {
		command, name string
		expectPrompt  bool
	}{
		{"\x03\xc0\xff\xc0\x03\r", "command mode", true},
		{"KISS ON\r", "KISS ON", true},
		{"RESTART\r", "RESTART", false},
	}
	for _, step := range steps {
		err := write(step.command)
		if err == nil && step.expectPrompt {
			err = prompt()
		}
		if err != nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			return nil, fmt.Errorf("Kenwood %s: %w", step.name, err)
		}
	}
	timer := time.NewTimer(settle)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("Kenwood restart: %w", ctx.Err())
	case <-timer.C:
	}
	return &bufferedPort{ReadWriteCloser: port, reader: reader}, nil
}

type bufferedPort struct {
	io.ReadWriteCloser
	reader *bufio.Reader
}

func (p *bufferedPort) Read(data []byte) (int, error) { return p.reader.Read(data) }
