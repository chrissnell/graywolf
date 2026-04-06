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
		Direction:  "input",
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
		InputDeviceID: dev.ID,
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

func TestChannelStatsCache(t *testing.T) {
	b := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	// Initially empty.
	if _, ok := b.GetChannelStats(0); ok {
		t.Fatal("expected no stats initially")
	}

	// Inject via the test helper.
	b.InjectStatusForTest(0, 100, 5, 10, 0.4, 0.3, 0.5, true)
	b.InjectStatusForTest(1, 200, 0, 20, 0.1, 0.1, 0.2, false)

	s0, ok := b.GetChannelStats(0)
	if !ok || s0.RxFrames != 100 || !s0.DcdState {
		t.Fatalf("unexpected stats for ch0: %+v", s0)
	}
	s1, ok := b.GetChannelStats(1)
	if !ok || s1.RxFrames != 200 || s1.DcdState {
		t.Fatalf("unexpected stats for ch1: %+v", s1)
	}

	all := b.GetAllChannelStats()
	if len(all) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(all))
	}
}

func TestReconfigureAudioDevice(t *testing.T) {
	store := seedStore(t)
	defer store.Close()

	b := New(Config{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	// Not running -> should fail.
	err := b.ReconfigureAudioDevice(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error when not running")
	}

	// Simulate RUNNING state with a fake send function that captures messages.
	b.setState(StateRunning)
	var sent []*pb.IpcMessage
	b.setSender(func(msg *pb.IpcMessage) error {
		sent = append(sent, msg)
		return nil
	})

	devices, _ := store.ListAudioDevices()
	if len(devices) == 0 {
		t.Fatal("no devices seeded")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = b.ReconfigureAudioDevice(ctx, devices[0].ID)
	if err != nil {
		t.Fatalf("ReconfigureAudioDevice: %v", err)
	}

	// Expect: StopAudio, ConfigureAudio, StartAudio
	if len(sent) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(sent))
	}
	if sent[0].GetStopAudio() == nil {
		t.Errorf("first message should be StopAudio, got %T", sent[0].GetPayload())
	}
	if sent[1].GetConfigureAudio() == nil {
		t.Errorf("second message should be ConfigureAudio, got %T", sent[1].GetPayload())
	}
	if sent[2].GetStartAudio() == nil {
		t.Errorf("third message should be StartAudio, got %T", sent[2].GetPayload())
	}
}

func TestReconfigureAudioDevice_BadDeviceID(t *testing.T) {
	store := seedStore(t)
	defer store.Close()

	b := New(Config{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	b.setState(StateRunning)
	b.setSender(func(msg *pb.IpcMessage) error { return nil })

	err := b.ReconfigureAudioDevice(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for nonexistent device")
	}
}

func TestStatusUpdatePopulatesCache(t *testing.T) {
	store := seedStore(t)
	defer store.Close()

	b := New(Config{
		Store:           store,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		FrameBufferSize: 16,
	})

	client, server := newPipePair()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Send ModemReady.
		_ = writeFrame(server, &pb.IpcMessage{Payload: &pb.IpcMessage_ModemReady{
			ModemReady: &pb.ModemReady{Version: "mock", Pid: 1},
		}})
		// Read config messages (ConfigureAudio, ConfigureChannel, ConfigurePtt, StartAudio).
		for i := 0; i < 4; i++ {
			if _, err := readFrame(server); err != nil {
				return
			}
		}
		// Send a StatusUpdate.
		_ = writeFrame(server, &pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{
			StatusUpdate: &pb.StatusUpdate{
				Channel: 1, RxFrames: 77, AudioLevelPeak: 0.8, DcdState: true,
			},
		}})
		// Wait for Shutdown.
		if m, err := readFrame(server); err == nil && m.GetShutdown() != nil {
			_ = writeFrame(server, &pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{
				StatusUpdate: &pb.StatusUpdate{ShutdownComplete: true},
			}})
		}
		_ = server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	sessionDone := make(chan error, 1)
	go func() {
		sessionDone <- b.runSession(ctx, client)
	}()

	// Wait briefly for the status update to be processed.
	time.Sleep(200 * time.Millisecond)

	stats, ok := b.GetChannelStats(1)
	if !ok {
		t.Fatal("expected stats for channel 1 after StatusUpdate")
	}
	if stats.RxFrames != 77 || !stats.DcdState {
		t.Errorf("unexpected stats: %+v", stats)
	}

	cancel()
	select {
	case <-sessionDone:
	case <-time.After(3 * time.Second):
		t.Fatal("session did not return")
	}
	<-done
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
