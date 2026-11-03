package tncprofile

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Model the distinct radio-control/TNC command sets, including the initial
// KISS/converse escape states and rejection of the other model's commands.
type modelRadioSpeed struct {
	rate             uint32
	state            *atomic.Uint32
	restoreAfter     time.Duration
	restored         chan struct{}
	wrongReadback    bool
	missFirstControl bool
	startupNoise     bool
}

func modelRadio(t *testing.T, profile, initial, band, rejectAt, id string, speed ...modelRadioSpeed) (io.ReadWriteCloser, <-chan error) {
	t.Helper()
	port, radio := net.Pipe()
	done := make(chan error, 1)
	responses := make(chan string, 32)
	var radioSpeed modelRadioSpeed
	if len(speed) > 0 {
		radioSpeed = speed[0]
	}
	go func() {
		for response := range responses {
			if _, err := io.WriteString(radio, response); err != nil {
				return
			}
		}
	}()
	go func() {
		defer radio.Close()
		defer close(responses)
		finish := func(err error) { done <- err }
		r := bufio.NewReader(radio)
		state := initial
		escapes := 0
		kissFrame := []byte{}
		for {
			b, err := r.ReadByte()
			if err != nil {
				finish(err)
				return
			}
			if state == "kiss" {
				kissFrame = append(kissFrame, b)
				if len(kissFrame) >= 3 && string(kissFrame[len(kissFrame)-3:]) == "\xc0\xff\xc0" {
					state = "command"
				}
			} else if b == 3 && state != "aprs" && state != "off" && state != "control" {
				escapes++
				if escapes >= 3 {
					state = "command"
				}
			}
			if b == '\r' {
				if state != "command" && state != "aprs" && state != "off" && state != "control" {
					finish(fmt.Errorf("CR sent before escaping %s", initial))
					return
				}
				break
			}
		}
		responses <- "\r?old synchronization noise\rcmd:"
		mode := "2"
		if initial == "aprs" {
			mode = "1"
		}
		if initial == "off" {
			mode = "0"
		}
		expected := []struct{ request, response string }{}
		if profile == TMD700 {
			expected = append(expected,
				struct{ request, response string }{"AI 1", "\rAI 1\r"},
				struct{ request, response string }{"TC 1", "\rTS 1\r"},
				struct{ request, response string }{"AI 1", "\rAI 1\r"},
				struct{ request, response string }{"ID", "\r" + id + "\r"},
				struct{ request, response string }{"TNC 2", "\rTNC 2\r"},
				struct{ request, response string }{"BC 0,0", "\rBC 0,0\r"},
				struct{ request, response string }{"DTB 0", "\rDTB 0\r"},
				struct{ request, response string }{"TC 0", ""}, struct{ request, response string }{"", "\rcmd:"})
		} else {
			if radioSpeed.missFirstControl {
				expected = append(expected, struct{ request, response string }{"TC 1", ""})
			}
			expected = append(expected, struct{ request, response string }{"TC 1", "\rTS 1\r"}, struct{ request, response string }{"ID", "\r" + id + "\r"})
			if id == "ID TM-D710P" {
				// Exact UI-View sequence: no bare TN query; force band A.
				band = "0"
			} else {
				expected = append(expected, struct{ request, response string }{"TN", "\rTN " + mode + "," + band + "\r"})
			}
			expected = append(expected, struct{ request, response string }{"TN 2," + band, "\rTN 2," + band + "\rcmd:"}, struct{ request, response string }{"", "\rcmd:"})
		}
		if radioSpeed.rate != 0 {
			expected = append(expected, struct{ request, response string }{fmt.Sprintf("HBAUD %d", radioSpeed.rate), fmt.Sprintf("HBAUD %d\rHBAUD      was 1200\rcmd:", radioSpeed.rate)}, struct{ request, response string }{"HBAUD", ""})
		}
		expected = append(expected, struct{ request, response string }{"ECHO OFF", "\rECHO     was ON\rcmd:"}, struct{ request, response string }{"KISS ON", "\rKISS     was OFF\r\ncmd:"}, struct{ request, response string }{"RESTART", ""})
		for _, step := range expected {
			request, err := r.ReadString('\r')
			if err != nil {
				finish(err)
				return
			}
			if request != step.request+"\r" {
				finish(fmt.Errorf("%s expected %q, got %q", profile, step.request, request))
				return
			}
			if step.request == rejectAt {
				responses <- "\r?EH\rcmd:"
				_, _ = io.Copy(io.Discard, r) // initialization must close on rejection
				finish(nil)
				return
			}
			if radioSpeed.state != nil {
				if step.request == "TNC 2" || strings.HasPrefix(step.request, "TN 2,") {
					go func() {
						time.Sleep(radioSpeed.restoreAfter)
						radioSpeed.state.Store(1200)
						close(radioSpeed.restored)
					}()
				}
				if strings.HasPrefix(step.request, "HBAUD ") {
					radioSpeed.state.Store(radioSpeed.rate)
				}
			}
			if step.request == "HBAUD" {
				actual := radioSpeed.rate
				if radioSpeed.state != nil {
					actual = radioSpeed.state.Load()
				}
				if radioSpeed.wrongReadback {
					actual = 1200
				}
				responses <- fmt.Sprintf("HBAUD %d\rcmd:", actual)
			}
			if step.request == "" && radioSpeed.startupNoise {
				responses <- "cmd:?EH\r\ncmd:HBAUD    was 1200\r\ncmd:"
			} else if step.response != "" {
				responses <- step.response
			}
		}
		// Reply only after RESTART: receiving the first KISS frame proves that
		// bytes were preserved across initialization, not just the test script.
		responses <- "\xc0\x00\x42\xc0"
		_, _ = io.Copy(io.Discard, r)
		finish(nil)
	}()
	return port, done
}

func TestModelProfilesAndInitialStates(t *testing.T) {
	for _, profile := range []string{TMD700, TMD710} {
		for _, initial := range []string{"command", "converse", "transparent", "kiss", "aprs", "off", "control"} {
			for _, band := range []string{"0", "1", "2", "3"} {
				if profile == TMD700 && band != "0" {
					continue
				}
				t.Run(profile+"/"+initial+"/"+band, func(t *testing.T) {
					id := "ID TM-D700"
					if profile == TMD710 {
						id = "ID TM-D710G"
					}
					port, done := modelRadio(t, profile, initial, band, "never", id)
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					stream, err := initialize(ctx, port, profile, 0)
					if err != nil {
						t.Fatal(err)
					}
					frame := make([]byte, 4)
					if _, err := io.ReadFull(stream, frame); err != nil {
						t.Fatal(err)
					}
					if string(frame) != "\xc0\x00\x42\xc0" {
						t.Fatalf("startup frame corrupted: %x", frame)
					}
					_ = stream.Close()
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestD710ModelSuffixes(t *testing.T) {
	for _, suffix := range []string{"", "A", "E", "P", "G", "GA", "GE"} {
		t.Run(suffix, func(t *testing.T) {
			port, done := modelRadio(t, TMD710, "control", "1", "never", "ID TM-D710"+suffix)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			stream, err := initialize(ctx, port, TMD710, 0)
			if err != nil {
				t.Fatal(err)
			}
			frame := make([]byte, 4)
			if _, err := io.ReadFull(stream, frame); err != nil || string(frame) != "\xc0\x00\x42\xc0" {
				t.Fatalf("KISS startup frame: %x, %v", frame, err)
			}
			_ = stream.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModelProfileRejectsCommandErrorAndWrongModel(t *testing.T) {
	for _, tc := range []struct{ profile, reject, id string }{
		{TMD700, "TNC 2", "ID TM-D700"},
		{TMD700, "BC 0,0", "ID TM-D700"},
		{TMD700, "DTB 0", "ID TM-D700"},
		{TMD710, "TN 2,1", "ID TM-D710"},
		{TMD710, "KISS ON", "ID TM-D710"},
		{TMD710, "RESTART", "ID TM-D710"},
		{TMD700, "never", "ID TM-D710"},
		{TMD710, "never", "ID TM-D700"},
		{TMD710, "never", "ID TM-D710UNKNOWN"},
		{TMD700, "never", "ID TM-D700P"},
	} {
		t.Run(tc.profile+tc.reject+tc.id, func(t *testing.T) {
			port, _ := modelRadio(t, tc.profile, "kiss", "1", tc.reject, tc.id)
			defer port.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := initialize(ctx, port, tc.profile, 50*time.Millisecond); err == nil || strings.Contains(err.Error(), "deadline") {
				t.Fatalf("expected explicit rejection, got %v", err)
			}
		})
	}
}

func TestModelProfileCancellationClosesBlockedPort(t *testing.T) {
	for _, profile := range []string{TMD700, TMD710} {
		t.Run(profile, func(t *testing.T) {
			port, radio := net.Pipe()
			defer radio.Close()
			// Block the very first write by never reading from the radio side.
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			_, err := initialize(ctx, port, profile, 0)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected cancellation, got %v", err)
			}
		})
	}
}

func TestD710RetriesControlCommandAfterKISSExit(t *testing.T) {
	port, done := modelRadio(t, TMD710, "control", "0", "never", "ID TM-D710P", modelRadioSpeed{missFirstControl: true, startupNoise: true})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := initialize(ctx, port, TMD710, 0)
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 4)
	if _, err := io.ReadFull(stream, frame); err != nil || string(frame) != "\xc0\x00\x42\xc0" {
		t.Fatalf("lost frame after retry: %x %v", frame, err)
	}
	_ = stream.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
