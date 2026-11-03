package kiss

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the real supervisor and its five-second settling period: a failed
// restart must back off without connecting; a later cable drop must rerun the
// model initialization on the new port rather than reusing a closed reader.
func TestModelSupervisorRejectRetryAndReconnect(t *testing.T) {
	for _, model := range []string{"kenwood-tm-d700", "kenwood-tm-d710"} {
		t.Run(model, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			var opens atomic.Int32
			var radios sync.WaitGroup
			restarted := make(chan int32, 8)
			connected := make(chan int32, 8)
			failures := make(chan error, 8)
			dropSecond := make(chan struct{})
			open := func(string, uint32) (io.ReadWriteCloser, error) {
				attempt := opens.Add(1)
				port, radio := net.Pipe()
				responses := make(chan string, 16)
				radios.Add(2)
				go func() {
					defer radios.Done()
					for response := range responses {
						for _, b := range []byte(response) {
							if _, err := radio.Write([]byte{b}); err != nil {
								return
							}
						}
					}
				}()
				go func() {
					defer radios.Done()
					defer radio.Close()
					defer close(responses)
					r := bufio.NewReader(radio)
					check := func(want string) bool {
						got := make([]byte, len(want))
						_, err := io.ReadFull(r, got)
						if err != nil || string(got) != want {
							if ctx.Err() == nil {
								failures <- fmt.Errorf("attempt %d expected %q got %q: %v", attempt, want, got, err)
							}
							return false
						}
						return true
					}
					if !check("\x03\x03\x03\xc0\xff\xc0") || !check("\x03\x03\x03\r") {
						return
					}
					steps := []struct{ request, response string }{}
					if model == "kenwood-tm-d700" {
						steps = append(steps, struct{ request, response string }{"AI 1", "AI 1\r"}, struct{ request, response string }{"TC 1", "TS 1\r"}, struct{ request, response string }{"AI 1", "AI 1\r"}, struct{ request, response string }{"ID", "ID TM-D700\r"}, struct{ request, response string }{"TNC 2", "TNC 2\r"}, struct{ request, response string }{"BC 0,0", "BC 0,0\r"}, struct{ request, response string }{"DTB 0", "DTB 0\r"}, struct{ request, response string }{"TC 0", ""}, struct{ request, response string }{"", "cmd:"})
					} else {
						steps = append(steps, struct{ request, response string }{"TC 1", "TS 1\r"}, struct{ request, response string }{"ID", "ID TM-D710G\r"}, struct{ request, response string }{"TN", "TN 2,1\r"}, struct{ request, response string }{"TN 2,1", "TN 2,1\rcmd:"}, struct{ request, response string }{"", "cmd:"})
					}
					steps = append(steps, struct{ request, response string }{"ECHO OFF", "ECHO was ON\rcmd:"}, struct{ request, response string }{"KISS ON", "KISS was OFF\rcmd:"})
					for _, step := range steps {
						if !check(step.request + "\r") {
							return
						}
						// Independent TX/RX emulates the buffering of a serial port.
						responses <- step.response
					}
					if !check("RESTART\r") {
						return
					}
					restarted <- attempt
					if attempt == 1 {
						responses <- "?EHcmd:"
						_, _ = io.Copy(io.Discard, r)
						return
					}
					if attempt == 2 {
						select {
						case <-dropSecond:
						case <-ctx.Done():
						}
						return
					}
					<-ctx.Done()
				}()
				return port, nil
			}
			sup := NewSerial(SerialConfig{Device: "model-test", BaudRate: 9600, InitProfile: model, ReconnectInitMs: 10, ReconnectMaxMs: 20, OpenFunc: open}, newTestSerialServer(t, &txSinkRecorder{}))
			sup.onReconnect = func() { connected <- opens.Load() }
			go sup.run(ctx)
			defer func() {
				cancel()
				sup.close()
				done := make(chan struct{})
				go func() { radios.Wait(); close(done) }()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("simulated radio leaked")
				}
			}()
			receive := func(events <-chan int32) int32 {
				select {
				case v := <-events:
					return v
				case err := <-failures:
					t.Fatal(err)
				case <-ctx.Done():
					t.Fatal("supervisor test timed out")
				}
				return 0
			}
			if receive(restarted) != 1 || receive(restarted) != 2 {
				t.Fatal("failed initialization was not retried")
			}
			if sup.Status().State != StateConnecting {
				t.Fatalf("connected before restart settled: %+v", sup.Status())
			}
			if got := receive(connected); got != 2 {
				t.Fatalf("failed restart counted as connected: %d", got)
			}
			waitState(t, sup, StateConnected)
			close(dropSecond)
			if receive(restarted) != 3 {
				t.Fatal("cable drop did not reinitialize")
			}
			if got := receive(connected); got != 3 {
				t.Fatalf("reconnection failed: %d", got)
			}
			waitState(t, sup, StateConnected)
			select {
			case err := <-failures:
				t.Fatal(err)
			default:
			}
		})
	}
}
