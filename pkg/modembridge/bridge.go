// Package modembridge supervises the Rust graywolf-modem child process and
// runs the IPC state machine that drives it from the Go side.
package modembridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/metrics"
)

// State names the current supervisor state.
type State int

const (
	StateStopped State = iota
	StateStarting
	StateConfiguring
	StateRunning
	StateRestarting
)

func (s State) String() string {
	switch s {
	case StateStopped:
		return "STOPPED"
	case StateStarting:
		return "STARTING"
	case StateConfiguring:
		return "CONFIGURING"
	case StateRunning:
		return "RUNNING"
	case StateRestarting:
		return "RESTARTING"
	default:
		return "?"
	}
}

// Config drives a Bridge.
type Config struct {
	// BinaryPath is the path to graywolf-modem. Defaults to
	// "./target/release/graywolf-modem".
	BinaryPath string
	// SocketDir is where the Unix socket file lives. Defaults to os.TempDir().
	SocketDir string
	// ReadinessTimeout bounds the wait for the child's stdout readiness byte.
	ReadinessTimeout time.Duration
	// ShutdownTimeout bounds graceful shutdown after a Shutdown IPC is sent.
	ShutdownTimeout time.Duration
	// Store supplies the channel/audio/ptt configuration to push to the child.
	Store configstore.ConfigStore
	// Metrics receives status updates and frame counts. Optional.
	Metrics *metrics.Metrics
	// Logger is used for structured logging. Defaults to slog.Default().
	Logger *slog.Logger
	// FrameBufferSize controls the capacity of the Frames() channel.
	FrameBufferSize int
	// DcdBufferSize controls the capacity of the DcdEvents() channel.
	DcdBufferSize int
}

func (c *Config) applyDefaults() {
	if c.BinaryPath == "" {
		c.BinaryPath = "./target/release/graywolf-modem"
	}
	if c.SocketDir == "" {
		c.SocketDir = os.TempDir()
	}
	if c.ReadinessTimeout == 0 {
		c.ReadinessTimeout = 5 * time.Second
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 5 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.FrameBufferSize == 0 {
		c.FrameBufferSize = 64
	}
	if c.DcdBufferSize == 0 {
		c.DcdBufferSize = 64
	}
}

// ChannelStats holds per-channel statistics sourced from StatusUpdate messages.
type ChannelStats struct {
	Channel         uint32  `json:"channel"`
	RxFrames        uint64  `json:"rx_frames"`
	RxBadFCS        uint64  `json:"rx_bad_fcs"`
	TxFrames        uint64  `json:"tx_frames"`
	DcdTransitions  uint64  `json:"dcd_transitions"`
	AudioLevelMark  float32 `json:"audio_level_mark"`
	AudioLevelSpace float32 `json:"audio_level_space"`
	AudioLevelPeak  float32 `json:"audio_level_peak"`
	DcdState        bool    `json:"dcd_state"`
}

// DeviceLevel holds the latest per-device audio level from the modem.
type DeviceLevel struct {
	DeviceID uint32  `json:"device_id"`
	PeakDBFS float32 `json:"peak_dbfs"`
	RmsDBFS  float32 `json:"rms_dbfs"`
	Clipping bool    `json:"clipping"`
}

// AvailableDevice describes an audio device discovered by cpal enumeration.
// Field names match the frontend's expected shape.
type AvailableDevice struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"` // cpal device name (used as device_path)
	SampleRates []uint32 `json:"sample_rates"`
	Channels    []uint32 `json:"channels"`
	HostAPI     string   `json:"host_api"`
	IsDefault   bool     `json:"is_default"`
	IsInput     bool     `json:"is_input"`
}

// RxHook is invoked for every received frame before it is delivered on
// the Frames() channel. It runs inline in the session goroutine, so
// implementations must be fast and non-blocking.
type RxHook func(*pb.ReceivedFrame)

// Bridge supervises the Rust modem child and exposes received frames to
// consumers.
type Bridge struct {
	cfg    Config
	logger *slog.Logger

	frames chan *pb.ReceivedFrame
	dcd    chan *pb.DcdChange

	mu    sync.Mutex
	state State

	// sendFn is the current session's write function, or nil if no session
	// is active. Guarded by mu.
	sendFn func(*pb.IpcMessage) error

	// Runtime fields guarded by mu.
	cancel context.CancelFunc
	done   chan struct{}

	// rxHook is invoked for every received frame before delivery.
	rxHook RxHook

	// Per-channel status cache, updated from StatusUpdate IPC messages.
	statusMu    sync.RWMutex
	statusCache map[uint32]*ChannelStats

	// dcdSubs is a fan-out list of subscriber channels for DCD events.
	// The original Bridge.dcd channel remains wired to the txgovernor;
	// DcdSubscribe appends an additional channel that the broadcaster
	// in supervise() writes to.
	dcdSubs []chan *pb.DcdChange

	// enumReqID is an atomic counter for EnumerateAudioDevices request IDs.
	// Also used for PlayTestTone request IDs.
	enumReqID atomic.Uint32
	// enumPending maps request_id → response channel for device enumeration.
	enumMu      sync.Mutex
	enumPending map[uint32]chan *pb.AudioDeviceList

	// tonePending maps request_id ��� response channel for test tone results.
	toneMu      sync.Mutex
	tonePending map[uint32]chan *pb.TestToneResult

	// Per-device audio level cache, updated from DeviceLevelUpdate IPC messages.
	deviceLevelMu    sync.RWMutex
	deviceLevelCache map[uint32]*DeviceLevel
}

// New builds a bridge. Call Start to run it.
func New(cfg Config) *Bridge {
	cfg.applyDefaults()
	return &Bridge{
		cfg:         cfg,
		logger:      cfg.Logger,
		frames:      make(chan *pb.ReceivedFrame, cfg.FrameBufferSize),
		dcd:         make(chan *pb.DcdChange, cfg.DcdBufferSize),
		state:       StateStopped,
		statusCache:      make(map[uint32]*ChannelStats),
		enumPending:      make(map[uint32]chan *pb.AudioDeviceList),
		tonePending:      make(map[uint32]chan *pb.TestToneResult),
		deviceLevelCache: make(map[uint32]*DeviceLevel),
	}
}

// DcdEvents returns a channel of DCD state-change events from the modem.
// Consumers (e.g. txgovernor) use these to time transmissions against
// channel-busy state. The channel is closed when Stop completes.
//
// Deprecated: for multi-subscriber use, call DcdSubscribe instead. This
// method remains as a compat shim for the existing txgovernor wiring
// and returns the primary channel.
func (b *Bridge) DcdEvents() <-chan *pb.DcdChange { return b.dcd }

// DcdSubscribe returns a new buffered channel that will receive every
// DcdChange event seen by the bridge. Multiple subscribers are
// supported; each receives a copy of every event. Slow subscribers
// will drop events (non-blocking send). The channel is closed when
// Stop completes.
func (b *Bridge) DcdSubscribe() <-chan *pb.DcdChange {
	ch := make(chan *pb.DcdChange, 32)
	b.mu.Lock()
	b.dcdSubs = append(b.dcdSubs, ch)
	b.mu.Unlock()
	return ch
}

// SetRxHook installs (or clears) the received-frame hook. Safe to call
// at any time; nil clears.
func (b *Bridge) SetRxHook(h RxHook) {
	b.mu.Lock()
	b.rxHook = h
	b.mu.Unlock()
}

// dispatchDcd sends ev to the primary channel and all subscribers.
// Non-blocking sends: a slow subscriber drops rather than stalls the
// modem session goroutine.
func (b *Bridge) dispatchDcd(ev *pb.DcdChange) {
	select {
	case b.dcd <- ev:
	default:
	}
	b.mu.Lock()
	subs := append([]chan *pb.DcdChange(nil), b.dcdSubs...)
	b.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- ev:
		default:
		}
	}
}

// dispatchRx calls the installed RxHook if any. Kept package-local so
// session.go can invoke it inline with its frame forwarder.
func (b *Bridge) dispatchRx(rf *pb.ReceivedFrame) {
	b.mu.Lock()
	h := b.rxHook
	b.mu.Unlock()
	if h != nil {
		h(rf)
	}
}

// SendTransmitFrame queues a TransmitFrame IPC message to the currently
// connected modem session. Returns an error if no session is active.
// Callers (e.g. the txgovernor) are expected to retry or drop on error.
func (b *Bridge) SendTransmitFrame(tf *pb.TransmitFrame) error {
	b.mu.Lock()
	send := b.sendFn
	b.mu.Unlock()
	if send == nil {
		return errors.New("modembridge: no active session")
	}
	return send(&pb.IpcMessage{Payload: &pb.IpcMessage_TransmitFrame{TransmitFrame: tf}})
}

func (b *Bridge) setSender(fn func(*pb.IpcMessage) error) {
	b.mu.Lock()
	b.sendFn = fn
	b.mu.Unlock()
}

// ConfigStore returns the attached configstore (may be nil).
func (b *Bridge) ConfigStore() configstore.ConfigStore { return b.cfg.Store }

// Frames returns a channel of received AX.25 frames. The channel is closed
// when Stop completes.
func (b *Bridge) Frames() <-chan *pb.ReceivedFrame { return b.frames }

// State returns the current supervisor state.
func (b *Bridge) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Bridge) setState(s State) {
	b.mu.Lock()
	b.state = s
	b.mu.Unlock()
	b.logger.Info("modembridge state", "state", s.String())
}

// Start launches the supervisor goroutine. It returns immediately.
func (b *Bridge) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.cancel != nil {
		b.mu.Unlock()
		return errors.New("modembridge: already started")
	}
	sctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.done = make(chan struct{})
	b.mu.Unlock()

	go b.supervise(sctx)
	return nil
}

// Stop cancels the supervisor and waits for it to exit.
func (b *Bridge) Stop() {
	b.mu.Lock()
	cancel := b.cancel
	done := b.done
	b.cancel = nil
	b.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// supervise is the top-level loop: spawn the child, drive one session, back
// off on error, repeat until the context is cancelled.
func (b *Bridge) supervise(ctx context.Context) {
	defer close(b.done)
	defer close(b.frames)
	defer close(b.dcd)
	defer func() {
		b.mu.Lock()
		subs := b.dcdSubs
		b.dcdSubs = nil
		b.mu.Unlock()
		for _, c := range subs {
			close(c)
		}
	}()

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			b.setState(StateStopped)
			return
		}
		b.setState(StateStarting)

		err := b.runOnce(ctx)
		if ctx.Err() != nil {
			b.setState(StateStopped)
			return
		}
		if err != nil {
			b.logger.Error("modembridge session ended", "err", err)
		}
		if b.cfg.Metrics != nil {
			b.cfg.Metrics.ChildRestarts.Inc()
			b.cfg.Metrics.SetChildUp(false)
		}

		b.setState(StateRestarting)
		select {
		case <-ctx.Done():
			b.setState(StateStopped)
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// runOnce brings the child up, runs one session, and tears it down. It
// returns whatever error caused the session to end (or nil on clean shutdown
// via context cancel).
func (b *Bridge) runOnce(ctx context.Context) error {
	sockPath := filepath.Join(b.cfg.SocketDir, fmt.Sprintf("graywolf-modem-%d.sock", os.Getpid()))
	_ = os.Remove(sockPath)

	cmd := exec.CommandContext(ctx, b.cfg.BinaryPath, sockPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", b.cfg.BinaryPath, err)
	}
	b.logger.Info("spawned modem", "pid", cmd.Process.Pid, "socket", sockPath)
	if b.cfg.Metrics != nil {
		b.cfg.Metrics.SetChildUp(true)
	}

	// Always ensure the child is cleaned up on return.
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
		_ = os.Remove(sockPath)
	}()

	// Readiness handshake: Rust writes exactly one '\n' byte to stdout once
	// the listener is bound.
	if err := waitForReadiness(stdout, b.cfg.ReadinessTimeout); err != nil {
		return fmt.Errorf("readiness: %w", err)
	}
	// Drain subsequent stdout so the pipe doesn't back up.
	go io.Copy(io.Discard, stdout)

	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", sockPath, err)
	}
	defer conn.Close()

	if err := b.runSession(ctx, conn); err != nil {
		return err
	}
	return nil
}

// GetChannelStats returns cached stats for a single channel.
func (b *Bridge) GetChannelStats(channel uint32) (*ChannelStats, bool) {
	b.statusMu.RLock()
	defer b.statusMu.RUnlock()
	s, ok := b.statusCache[channel]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

// GetAllChannelStats returns cached stats for all channels.
func (b *Bridge) GetAllChannelStats() map[uint32]*ChannelStats {
	b.statusMu.RLock()
	defer b.statusMu.RUnlock()
	out := make(map[uint32]*ChannelStats, len(b.statusCache))
	for k, v := range b.statusCache {
		cp := *v
		out[k] = &cp
	}
	return out
}

// updateStatusCache stores the latest StatusUpdate for a channel.
func (b *Bridge) updateStatusCache(s *pb.StatusUpdate) {
	b.statusMu.Lock()
	defer b.statusMu.Unlock()
	b.statusCache[s.Channel] = &ChannelStats{
		Channel:         s.Channel,
		RxFrames:        s.RxFrames,
		RxBadFCS:        s.RxBadFcs,
		TxFrames:        s.TxFrames,
		DcdTransitions:  s.DcdTransitions,
		AudioLevelMark:  s.AudioLevelMark,
		AudioLevelSpace: s.AudioLevelSpace,
		AudioLevelPeak:  s.AudioLevelPeak,
		DcdState:        s.DcdState,
	}
}

// ReconfigureAudioDevice performs a hot-swap of an audio device's configuration.
// Sequence: StopAudio -> wait -> ConfigureAudio -> StartAudio.
// Only the specified device is reconfigured; unaffected channels continue.
func (b *Bridge) ReconfigureAudioDevice(ctx context.Context, deviceID uint32) error {
	if b.State() != StateRunning {
		return errors.New("modembridge: not in RUNNING state")
	}
	store := b.cfg.Store
	if store == nil {
		return errors.New("modembridge: no configstore")
	}
	dev, err := store.GetAudioDevice(deviceID)
	if err != nil {
		return fmt.Errorf("get audio device %d: %w", deviceID, err)
	}

	// Stop audio processing.
	if err := b.sendIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StopAudio{StopAudio: &pb.StopAudio{}}}); err != nil {
		return fmt.Errorf("send StopAudio: %w", err)
	}

	// Brief pause for the modem to finish audio teardown.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(200 * time.Millisecond):
	}

	// Send updated ConfigureAudio for this device.
	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_ConfigureAudio{ConfigureAudio: &pb.ConfigureAudio{
		DeviceId:   dev.ID,
		DeviceName: audioDeviceName(dev),
		SampleRate: dev.SampleRate,
		Channels:   dev.Channels,
		SourceType: dev.SourceType,
		Format:     dev.Format,
		GainDb:     dev.GainDB,
	}}}
	if err := b.sendIPC(msg); err != nil {
		return fmt.Errorf("send ConfigureAudio: %w", err)
	}

	// Re-start audio.
	if err := b.sendIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StartAudio{StartAudio: &pb.StartAudio{}}}); err != nil {
		return fmt.Errorf("send StartAudio: %w", err)
	}
	return nil
}

// EnumerateAudioDevices asks the Rust modem to list available audio devices
// via cpal and waits for the response. Returns nil slice if the bridge is not
// running or the request times out.
func (b *Bridge) EnumerateAudioDevices(ctx context.Context) ([]AvailableDevice, error) {
	if b.State() != StateRunning {
		return nil, errors.New("modembridge: not in RUNNING state")
	}

	reqID := b.enumReqID.Add(1)
	ch := make(chan *pb.AudioDeviceList, 1)

	b.enumMu.Lock()
	b.enumPending[reqID] = ch
	b.enumMu.Unlock()
	defer func() {
		b.enumMu.Lock()
		delete(b.enumPending, reqID)
		b.enumMu.Unlock()
	}()

	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_EnumerateAudioDevices{
		EnumerateAudioDevices: &pb.EnumerateAudioDevices{
			RequestId:     reqID,
			IncludeOutput: false, // input devices only for now
		},
	}}
	if err := b.sendIPC(msg); err != nil {
		return nil, fmt.Errorf("send EnumerateAudioDevices: %w", err)
	}

	// Wait for response with timeout.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return convertDeviceList(resp), nil
	case <-timer.C:
		return nil, errors.New("modembridge: enumerate timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// dispatchEnumResponse delivers an AudioDeviceList to the waiting caller.
func (b *Bridge) dispatchEnumResponse(list *pb.AudioDeviceList) {
	b.enumMu.Lock()
	ch, ok := b.enumPending[list.RequestId]
	b.enumMu.Unlock()
	if ok {
		select {
		case ch <- list:
		default:
		}
	}
}

func convertDeviceList(list *pb.AudioDeviceList) []AvailableDevice {
	out := make([]AvailableDevice, 0, len(list.Devices))
	for _, d := range list.Devices {
		out = append(out, AvailableDevice{
			Name:        d.Name,
			Path:        d.Name, // cpal device name is the path
			SampleRates: d.SampleRates,
			Channels:    d.ChannelCounts,
			HostAPI:     d.HostApi,
			IsDefault:   d.IsDefault,
			IsInput:     d.Kind == pb.AudioDeviceKind_AUDIO_DEVICE_KIND_INPUT,
		})
	}
	return out
}

// PlayTestTone asks the Rust modem to play a test tone on the named output
// device and waits for the result. Follows the same request/response pattern
// as EnumerateAudioDevices.
func (b *Bridge) PlayTestTone(ctx context.Context, deviceID uint32, deviceName string, sampleRate, channels uint32) error {
	if b.State() != StateRunning {
		return errors.New("modembridge: not in RUNNING state")
	}

	reqID := b.enumReqID.Add(1)
	ch := make(chan *pb.TestToneResult, 1)

	b.toneMu.Lock()
	b.tonePending[reqID] = ch
	b.toneMu.Unlock()
	defer func() {
		b.toneMu.Lock()
		delete(b.tonePending, reqID)
		b.toneMu.Unlock()
	}()

	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_PlayTestTone{
		PlayTestTone: &pb.PlayTestTone{
			RequestId:  reqID,
			DeviceName: deviceName,
			SampleRate: sampleRate,
			Channels:   channels,
			DeviceId:   deviceID,
		},
	}}
	if err := b.sendIPC(msg); err != nil {
		return fmt.Errorf("send PlayTestTone: %w", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case resp := <-ch:
		if !resp.Success {
			return fmt.Errorf("test tone failed: %s", resp.Error)
		}
		return nil
	case <-timer.C:
		return errors.New("modembridge: test tone timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dispatchToneResponse delivers a TestToneResult to the waiting caller.
func (b *Bridge) dispatchToneResponse(r *pb.TestToneResult) {
	b.toneMu.Lock()
	ch, ok := b.tonePending[r.RequestId]
	b.toneMu.Unlock()
	if ok {
		select {
		case ch <- r:
		default:
		}
	}
}

// SetDeviceGain sends a live gain adjustment to the modem (fire-and-forget).
func (b *Bridge) SetDeviceGain(deviceID uint32, gainDB float32) error {
	return b.sendIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_SetDeviceGain{
		SetDeviceGain: &pb.SetDeviceGain{
			DeviceId: deviceID,
			GainDb:   gainDB,
		},
	}})
}

// updateDeviceLevelCache stores the latest DeviceLevelUpdate for a device.
func (b *Bridge) updateDeviceLevelCache(u *pb.DeviceLevelUpdate) {
	b.deviceLevelMu.Lock()
	defer b.deviceLevelMu.Unlock()
	b.deviceLevelCache[u.DeviceId] = &DeviceLevel{
		DeviceID: u.DeviceId,
		PeakDBFS: u.PeakDbfs,
		RmsDBFS:  u.RmsDbfs,
		Clipping: u.Clipping,
	}
}

// GetAllDeviceLevels returns the latest cached audio levels for all devices.
func (b *Bridge) GetAllDeviceLevels() map[uint32]*DeviceLevel {
	b.deviceLevelMu.RLock()
	defer b.deviceLevelMu.RUnlock()
	out := make(map[uint32]*DeviceLevel, len(b.deviceLevelCache))
	for k, v := range b.deviceLevelCache {
		cp := *v
		out[k] = &cp
	}
	return out
}

// sendIPC writes an IPC message using the current session's send function.
func (b *Bridge) sendIPC(msg *pb.IpcMessage) error {
	b.mu.Lock()
	fn := b.sendFn
	b.mu.Unlock()
	if fn == nil {
		return errors.New("modembridge: not connected")
	}
	return fn(msg)
}

// InjectStatusForTest populates the status cache directly. Test-only.
func (b *Bridge) InjectStatusForTest(channel uint32, rxFrames, rxBadFCS, txFrames uint64,
	markLevel, spaceLevel, peakLevel float32, dcd bool) {
	b.statusMu.Lock()
	defer b.statusMu.Unlock()
	b.statusCache[channel] = &ChannelStats{
		Channel:         channel,
		RxFrames:        rxFrames,
		RxBadFCS:        rxBadFCS,
		TxFrames:        txFrames,
		AudioLevelMark:  markLevel,
		AudioLevelSpace: spaceLevel,
		AudioLevelPeak:  peakLevel,
		DcdState:        dcd,
	}
}

// waitForReadiness blocks until the child writes a byte (expected '\n') to
// stdout or the timeout expires.
func waitForReadiness(r io.Reader, timeout time.Duration) error {
	type result struct {
		b   byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		br := bufio.NewReader(r)
		b, err := br.ReadByte()
		ch <- result{b, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			return res.err
		}
		if res.b != '\n' {
			return fmt.Errorf("unexpected readiness byte %q", res.b)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout after %s", timeout)
	}
}
