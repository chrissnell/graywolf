package tncprofile

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// The radio-control command sets differ even though both TNCs accept KISS ON.
// D700: https://www.on7lds.net/auto/tmd700a.htm (N0RMO's mode experiments).
// D710: Kenwood TM-D710AE_IDM_R1.pdf, section 5.1.3; TN mode/band query
// documented by LA3QMA/TM-V71_TM-D710-Kenwood, commands/TN.md.
func initializeModel(parent context.Context, port io.ReadWriteCloser, profile string, radioBaud uint32, modeSettle, settle time.Duration) (result io.ReadWriteCloser, err error) {
	// Keep a single raw reader across command retries and transport handoff.
	// A per-command timeout must never leave a competing blocked serial read.
	pump := newRestartPort(port, port)
	port = pump
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	stopClose := context.AfterFunc(ctx, func() { _ = port.Close() })
	defer stopClose()
	defer func() {
		if err != nil {
			_ = port.Close()
			if ctx.Err() != nil {
				err = fmt.Errorf("%s initialization: %w (%v)", profile, ctx.Err(), err)
			}
		}
	}()
	input := &kenwoodContextReader{port: pump, ctx: ctx}
	r := bufio.NewReader(input)
	write := func(s string) error {
		n, err := io.WriteString(port, s)
		if err == nil && n != len(s) {
			err = io.ErrShortWrite
		}
		return err
	}
	pause := func(delay time.Duration) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	// No CR is sent until control characters have escaped converse/transparent
	// mode. KISS return is binary; it is never sent as a printable command.
	if err = write("\x03\x03\x03\xc0\xff\xc0"); err != nil {
		return nil, err
	}
	if err = pause(200 * time.Millisecond); err != nil {
		return nil, err
	}
	if err = write("\x03\x03\x03\r"); err != nil {
		return nil, err
	}
	if profile == TMD700 {
		// UI-View sends AI before TC. In TNC command mode AI can be
		// rejected or merely echoed; require the distinct TC response first.
		if err = write("AI 1\r"); err != nil {
			return nil, err
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err = write("TC 1\r"); err != nil {
			return nil, err
		}
		stepCtx, stepCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		input.ctx = stepCtx
		_, err = readKenwoodResponse(r, false, func(s string) bool { return s == "TS 1" })
		stepCancel()
		input.ctx = ctx
		if err == nil {
			break
		}
		if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%s TC 1: %w", profile, err)
	}
	if profile == TMD700 {
		// Now AI 1 is a radio-control command rather than a TNC echo.
		if err = write("AI 1\r"); err != nil {
			return nil, err
		}
		if _, err = readKenwoodResponse(r, true, func(s string) bool { return s == "AI 1" }); err != nil {
			return nil, fmt.Errorf("TM-D700 AI 1: %w", err)
		}
	}
	if err = write("ID\r"); err != nil {
		return nil, err
	}
	model, err := readKenwoodResponse(r, true, func(s string) bool { return strings.HasPrefix(s, "ID ") })
	if err != nil {
		return nil, fmt.Errorf("%s ID: %w", profile, err)
	}
	expected := "ID TM-D700"
	if profile == TMD710 {
		expected = "ID TM-D710"
	}
	suffix := strings.TrimPrefix(model, expected)
	allowedSuffix := suffix == "" || suffix == "A" || suffix == "E" || (profile == TMD710 && (suffix == "P" || suffix == "G" || suffix == "GA" || suffix == "GE"))
	if !strings.HasPrefix(model, expected) || !allowedSuffix {
		return nil, fmt.Errorf("%s: unexpected radio model %q", profile, model)
	}
	command := func(request string, prompt bool, match func(string) bool) error {
		if err := write(request + "\r"); err != nil {
			return err
		}
		if prompt {
			_, err := readKenwoodPrompt(r, request == "KISS ON")
			return err
		}
		_, err := readKenwoodResponse(r, true, match)
		return err
	}
	if profile == TMD700 {
		if err = command("TNC 2", false, func(s string) bool { return s == "TNC 2" }); err != nil {
			return nil, fmt.Errorf("TM-D700 TNC 2: %w", err)
		}
		if err = pause(modeSettle); err != nil {
			return nil, err
		}
		// Select operating/data band A as in the supplied UI-View file.
		// Frequency and shift remain the operator's radio settings.
		for _, request := range []string{"BC 0,0", "DTB 0"} {
			if err = command(request, false, func(s string) bool { return s == request }); err != nil {
				return nil, fmt.Errorf("TM-D700 %s: %w", request, err)
			}
		}
		if err = write("TC 0\r"); err != nil {
			return nil, err
		}
		if err = pause(200 * time.Millisecond); err != nil {
			return nil, err
		}
		if err = command("", true, nil); err != nil {
			return nil, fmt.Errorf("TM-D700 TC 0: %w", err)
		}
	} else {
		// The TM-D710P rejects a bare TN query. Its working UI-View
		// initialization selects PACKET directly on band A (docs/TM-D710.cmd).
		request := "TN 2,0"
		if model != "ID TM-D710P" {
			if err = write("TN\r"); err != nil {
				return nil, err
			}
			state, err := readKenwoodResponse(r, true, func(s string) bool { return strings.HasPrefix(s, "TN ") })
			if err != nil {
				return nil, fmt.Errorf("TM-D710 TN: %w", err)
			}
			parts := strings.Split(strings.TrimPrefix(state, "TN "), ",")
			if len(parts) != 2 || len(parts[0]) != 1 || !strings.Contains("012", parts[0]) || len(parts[1]) != 1 || !strings.Contains("0123", parts[1]) {
				return nil, fmt.Errorf("TM-D710: invalid TNC mode/band %q", state)
			}
			request = "TN 2," + parts[1]
		}
		if err = command(request, false, func(s string) bool { return s == request }); err != nil {
			return nil, fmt.Errorf("TM-D710 %s: %w", request, err)
		}
		// Some firmware restores its old TNC speed/state for 3-4 seconds
		// after TN, even if a command prompt has already appeared.
		if err = pause(modeSettle); err != nil {
			return nil, err
		}
		if err = command("", true, nil); err != nil {
			return nil, fmt.Errorf("TM-D710 PACKET prompt: %w", err)
		}
	}
	// TN may emit several prompts and saved-setting replies after its first
	// cmd:. Drain only this startup transcript before issuing our settings;
	// a delayed ?EH from synchronization must not reject ECHO OFF or HBAUD.
	startupEnd := time.Now().Add(time.Second)
	for time.Now().Before(startupEnd) {
		quietCtx, quietCancel := context.WithDeadline(ctx, minTime(startupEnd, time.Now().Add(200*time.Millisecond)))
		input.ctx = quietCtx
		var discard [1024]byte
		_, readErr := r.Read(discard[:])
		quietCancel()
		input.ctx = ctx
		if errors.Is(readErr, context.DeadlineExceeded) && ctx.Err() == nil {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("%s startup drain: %w", profile, readErr)
		}
	}
	if radioBaud != 0 {
		if err = write(fmt.Sprintf("HBAUD %d\r", radioBaud)); err != nil {
			return nil, err
		}
		_, err = readKenwoodPromptMatching(r, func(line string) bool {
			fields := strings.Fields(strings.ToUpper(line))
			return len(fields) == 3 && fields[0] == "HBAUD" && fields[1] == "WAS" && (fields[2] == "1200" || fields[2] == "9600")
		})
		if err != nil {
			return nil, fmt.Errorf("%s HBAUD setting: %w", profile, err)
		}
		// A setting reply reports the previous value. Query the current value
		// separately; a stale cmd: cannot count as successful verification.
		if err = write("HBAUD\r"); err != nil {
			return nil, err
		}
		value, readErr := readKenwoodResponse(r, true, func(s string) bool {
			return strings.HasPrefix(s, "HBAUD ") && !strings.Contains(strings.ToUpper(s), "WAS")
		})
		if readErr != nil {
			return nil, fmt.Errorf("%s HBAUD verification: %w", profile, readErr)
		}
		if strings.Join(strings.Fields(value), " ") != fmt.Sprintf("HBAUD %d", radioBaud) {
			return nil, fmt.Errorf("%s HBAUD verification: requested %d, got %q", profile, radioBaud, value)
		}
		if _, err = readKenwoodPrompt(r, false); err != nil {
			return nil, fmt.Errorf("%s HBAUD prompt: %w", profile, err)
		}
	}
	// Disable command echo before KISS so an echoed command plus a stale
	// prompt cannot be mistaken for the TNC's setting acknowledgement.
	if err = write("ECHO OFF\r"); err != nil {
		return nil, err
	}
	if _, err = readKenwoodPromptMatching(r, func(line string) bool {
		text := strings.Join(strings.Fields(strings.ToUpper(line)), " ")
		return text == "ECHO WAS ON" || text == "ECHO WAS OFF"
	}); err != nil {
		return nil, fmt.Errorf("%s ECHO OFF: %w", profile, err)
	}
	if err = command("KISS ON", true, nil); err != nil {
		return nil, fmt.Errorf("%s KISS ON: %w", profile, err)
	}
	if err = write("RESTART\r"); err != nil {
		return nil, err
	}
	input.ctx = context.Background() // runtime reads outlive initialization
	stream := newRestartPort(port, r)
	if err = stream.waitRestart(ctx, settle); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("%s RESTART: %w", profile, err)
	}
	// KISS has no mandatory startup acknowledgement. This means the serial
	// session is ready, not that an RF exchange has been validated.
	return newKenwoodSession(stream, len(stream.pending)), nil
}

func kenwoodRejected(line string) bool {
	line = strings.ToUpper(strings.TrimSpace(line))
	return strings.HasPrefix(line, "?") || line == "N" || strings.HasPrefix(line, "UNKNOWN") || strings.HasPrefix(line, "ERROR")
}

func readKenwoodResponse(r *bufio.Reader, reject bool, match func(string) bool) (string, error) {
	line := ""
	for i := 0; i < 8192; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '\r' || b == '\n' {
			text := strings.TrimSpace(line)
			line = ""
			if reject && kenwoodRejected(text) {
				return "", fmt.Errorf("radio rejected command: %s", text)
			}
			if match(text) {
				return text, nil
			}
		} else if b == '\t' || (b >= 32 && b < 127) {
			line += string(b)
		}
		if strings.HasSuffix(strings.ToLower(line), "cmd:") {
			before := strings.TrimSpace(line[:len(line)-4])
			if reject && kenwoodRejected(before) {
				return "", fmt.Errorf("radio rejected command: %s", before)
			}
			line = ""
		}
	}
	return "", fmt.Errorf("expected radio response not found within 8192 bytes")
}

func readKenwoodPrompt(r *bufio.Reader, requireKISS bool) (string, error) {
	var match func(string) bool
	if requireKISS {
		match = kenwoodKISSAcknowledgement
	}
	return readKenwoodPromptMatching(r, match)
}

func readKenwoodPromptMatching(r *bufio.Reader, match func(string) bool) (string, error) {
	line := ""
	acknowledged := match == nil
	for i := 0; i < 8192; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '\r' || b == '\n' {
			if kenwoodRejected(line) {
				return "", fmt.Errorf("radio rejected command: %s", strings.TrimSpace(line))
			}
			if match != nil && match(line) {
				acknowledged = true
			}
			line = ""
		} else if b == '\t' || (b >= 32 && b < 127) {
			line += string(b)
		}
		if strings.HasSuffix(strings.ToLower(line), "cmd:") {
			before := line[:len(line)-4]
			if kenwoodRejected(before) {
				return "", fmt.Errorf("radio rejected command: %s", before)
			}
			if match != nil && match(before) {
				acknowledged = true
			}
			if acknowledged {
				return line, nil
			}
			line = "" // A stale startup prompt does not acknowledge KISS ON.
		}
	}
	return "", fmt.Errorf("command prompt not found within 8192 bytes")
}

func kenwoodKISSAcknowledgement(line string) bool {
	switch strings.Join(strings.Fields(strings.ToUpper(line)), " ") {
	case "KISS ON", "KISS WAS OFF", "KISS WAS ON":
		return true
	default:
		return false
	}
}

// Used only by the initialization goroutine until runtime handoff.
type kenwoodContextReader struct {
	port *restartPort
	ctx  context.Context
}

func (r *kenwoodContextReader) Read(dst []byte) (int, error) {
	return r.port.readContext(r.ctx, dst)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
