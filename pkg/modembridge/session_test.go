package modembridge

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// fakeConn implements sessionConn over two io.Pipes (one per direction).
type fakeConn struct {
	r      *io.PipeReader
	w      *io.PipeWriter
	closed bool
}

func (f *fakeConn) Read(p []byte) (int, error)     { return f.r.Read(p) }
func (f *fakeConn) Write(p []byte) (int, error)    { return f.w.Write(p) }
func (f *fakeConn) SetReadDeadline(time.Time) error { return nil }
func (f *fakeConn) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	_ = f.r.Close()
	_ = f.w.Close()
	return nil
}

// newPipePair returns two connected fakeConns. Writes to a are readable from b
// and vice versa.
func newPipePair() (clientSide, serverSide *fakeConn) {
	aR, bW := io.Pipe() // server writes here, client reads here
	bR, aW := io.Pipe() // client writes here, server reads here
	return &fakeConn{r: aR, w: aW}, &fakeConn{r: bR, w: bW}
}

func seedStore(t *testing.T) *configstore.Store {
	t.Helper()
	s, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	dev := &configstore.AudioDevice{
		Name:       "test",
		SourceType: "flac",
		SourcePath: "/tmp/does-not-exist.flac",
		SampleRate: 44100,
		Channels:   1,
		Format:     "s16le",
	}
	if err := s.CreateAudioDevice(dev); err != nil {
		t.Fatal(err)
	}
	ch := &configstore.Channel{
		Name:          "rx1",
		AudioDeviceID: dev.ID,
		ModemType:     "afsk",
		BitRate:       1200,
		MarkFreq:      1200,
		SpaceFreq:     2200,
		Profile:       "A",
		NumSlicers:    1,
		FixBits:       "none",
	}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunSessionHappyPath(t *testing.T) {
	store := seedStore(t)
	defer store.Close()

	b := New(Config{
		Store:           store,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		FrameBufferSize: 16,
	})
	// Frames channel is closed in supervise; for standalone session tests we
	// only consume from it, so closing at test end is fine.

	client, server := newPipePair()

	// Fake modem goroutine: send ModemReady, then a ReceivedFrame after we've
	// seen the Configure messages, then close on Shutdown.
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 1) send ModemReady
		if err := writeFrame(server, &pb.IpcMessage{Payload: &pb.IpcMessage_ModemReady{
			ModemReady: &pb.ModemReady{Version: "mock", Pid: 1},
		}}); err != nil {
			t.Errorf("write ModemReady: %v", err)
			return
		}
		// 2) expect ConfigureAudio, ConfigureChannel, ConfigurePtt, StartAudio
		expected := []string{"ConfigureAudio", "ConfigureChannel", "ConfigurePtt", "StartAudio"}
		for _, want := range expected {
			m, err := readFrame(server)
			if err != nil {
				t.Errorf("read %s: %v", want, err)
				return
			}
			got := ""
			switch m.GetPayload().(type) {
			case *pb.IpcMessage_ConfigureAudio:
				got = "ConfigureAudio"
			case *pb.IpcMessage_ConfigureChannel:
				got = "ConfigureChannel"
			case *pb.IpcMessage_ConfigurePtt:
				got = "ConfigurePtt"
			case *pb.IpcMessage_StartAudio:
				got = "StartAudio"
			}
			if got != want {
				t.Errorf("sequence mismatch: got %s, want %s", got, want)
				return
			}
		}
		// 3) Emit a ReceivedFrame.
		_ = writeFrame(server, &pb.IpcMessage{Payload: &pb.IpcMessage_ReceivedFrame{
			ReceivedFrame: &pb.ReceivedFrame{Channel: 1, Data: []byte{0xAA, 0xBB}, Retry: "none"},
		}})
		// 4) Wait for Shutdown.
		m, err := readFrame(server)
		if err != nil {
			return
		}
		if m.GetShutdown() == nil {
			t.Errorf("expected Shutdown, got %T", m.GetPayload())
		}
		// 5) Emit final StatusUpdate and close.
		_ = writeFrame(server, &pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{
			StatusUpdate: &pb.StatusUpdate{ShutdownComplete: true},
		}})
		_ = server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	sessionDone := make(chan error, 1)
	go func() {
		sessionDone <- b.runSession(ctx, client)
	}()

	// Wait to receive the frame from the bridge's Frames() channel.
	select {
	case f := <-b.frames:
		if f.Channel != 1 || len(f.Data) != 2 {
			t.Errorf("unexpected frame: %+v", f)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for frame")
	}

	// Trigger graceful shutdown.
	cancel()

	select {
	case err := <-sessionDone:
		if err != nil {
			t.Errorf("runSession: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runSession did not return after cancel")
	}
	<-done
}

func TestRunSessionRejectsNonReadyFirstMessage(t *testing.T) {
	store := seedStore(t)
	defer store.Close()
	b := New(Config{Store: store, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	client, server := newPipePair()
	go func() {
		// Send wrong first message.
		_ = writeFrame(server, &pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{
			StatusUpdate: &pb.StatusUpdate{},
		}})
	}()
	err := b.runSession(context.Background(), client)
	if err == nil {
		t.Fatal("expected error for missing ModemReady")
	}
}

func TestStateTransitions(t *testing.T) {
	b := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if b.State() != StateStopped {
		t.Fatalf("initial state: %s", b.State())
	}
	b.setState(StateStarting)
	if b.State() != StateStarting {
		t.Fatalf("after setState: %s", b.State())
	}
}
