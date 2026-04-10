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
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
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
	// "./target/release/graywolf-modem" (the workspace-shared cargo
	// output directory at the repo root).
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
	Description string   `json:"description"` // human-friendly name (e.g. USB product string)
	Path        string   `json:"path"`        // cpal device name (used as device_path)
	SampleRates []uint32 `json:"sample_rates"`
	Channels    []uint32 `json:"channels"`
	HostAPI     string   `json:"host_api"`
	IsDefault   bool     `json:"is_default"`
	IsInput     bool     `json:"is_input"`
}

// Bridge supervises the Rust modem child and exposes received frames to
// consumers.
type Bridge struct {
	cfg    Config
	logger *slog.Logger

	frames chan *pb.ReceivedFrame

	// dcd is the per-Bridge DCD publisher. It owns the primary subscriber
	// channel returned by DcdEvents() (held in dcdPrimary), plus any
	// additional DcdSubscribe() subscribers. It is Closed from supervise's
	// defer chain once, at Stop; each subscriber's range consumer exits
	// then.
	dcd        *dcdPublisher
	dcdPrimary <-chan *pb.DcdChange

	mu    sync.Mutex
	state State

	// sendFn is the current session's write function, or nil if no session
	// is active. Guarded by mu.
	sendFn func(*pb.IpcMessage) error

	// Runtime fields guarded by mu.
	cancel context.CancelFunc
	done   chan struct{}

	// status owns the per-channel ChannelStats and per-device DeviceLevel
	// caches. Reset at the top of every supervise iteration so stale
	// state from a crashed child does not survive a restart.
	status *statusCache

	// Generic dispatchers correlate per-request IDs with reply channels
	// for the three request/response IPC exchanges the bridge supports.
	// supervise() calls Reset on each at the top of every iteration and
	// Close in its defer, so callers blocked in their per-call select
	// unblock with a zero-value response (treated as errBridgeStopped)
	// rather than waiting for their per-call timeout.
	enumDispatcher *dispatcher[*pb.AudioDeviceList]
	toneDispatcher *dispatcher[*pb.TestToneResult]
	scanDispatcher *dispatcher[*pb.InputLevelScanResult]

	// stdoutRing is a bounded ring buffer of the most recent lines the
	// child wrote to stdout. It replaces the unbounded
	// io.Copy(io.Discard, stdout) drain so crash diagnostics can show
	// the child's final output, and so the reader goroutine does not
	// leak if the child deadlocks on stdout.
	stdoutMu   sync.Mutex
	stdoutRing []string
}

// stdoutRingMax is the maximum number of lines retained in the stdout
// ring buffer. Sized to capture a typical Rust panic trace plus a few
// surrounding log lines.
const stdoutRingMax = 16

// New builds a bridge. Call Start to run it.
func New(cfg Config) *Bridge {
	cfg.applyDefaults()
	var incDcdDropped func()
	// Metrics does not yet carry a DCD-drop counter; if/when it does the
	// Bridge can pass a direct increment here. For now drops are logged
	// at debug with a 10s rate limit inside dcdPublisher.
	_ = incDcdDropped
	pub := newDcdPublisher(cfg.Logger, incDcdDropped)
	b := &Bridge{
		cfg:            cfg,
		logger:         cfg.Logger,
		frames:         make(chan *pb.ReceivedFrame, cfg.FrameBufferSize),
		dcd:            pub,
		state:          StateStopped,
		status:         newStatusCache(),
		enumDispatcher: newDispatcher[*pb.AudioDeviceList](),
		toneDispatcher: newDispatcher[*pb.TestToneResult](),
		scanDispatcher: newDispatcher[*pb.InputLevelScanResult](),
	}
	// Hold a long-lived "primary" subscription so DcdEvents() can return
	// a stable channel for the txgovernor wiring path that predates
	// DcdSubscribe. The publisher closes it alongside the other
	// subscribers at Stop time.
	b.dcdPrimary = pub.Subscribe()
	return b
}

// DcdEvents returns a channel of DCD state-change events from the modem.
// Consumers (e.g. txgovernor) use these to time transmissions against
// channel-busy state. The channel is closed when Stop completes.
//
// Deprecated: for multi-subscriber use, call DcdSubscribe instead. This
// method remains as a compat shim for the existing txgovernor wiring
// and returns the long-lived primary subscription allocated in New.
func (b *Bridge) DcdEvents() <-chan *pb.DcdChange { return b.dcdPrimary }

// DcdSubscribe returns a new buffered channel that will receive every
// DcdChange event seen by the bridge. Multiple subscribers are
// supported; each receives a copy of every event. Slow subscribers will
// drop events (non-blocking send). The channel is closed when Stop
// completes or when the caller passes it to DcdUnsubscribe.
func (b *Bridge) DcdSubscribe() <-chan *pb.DcdChange { return b.dcd.Subscribe() }

// DcdUnsubscribe removes a previously Subscribed channel and closes it
// so the caller's range loop exits. Callers that drop their subscription
// without calling this method leak memory on every Publish fan-out.
func (b *Bridge) DcdUnsubscribe(ch <-chan *pb.DcdChange) { b.dcd.Unsubscribe(ch) }

// dispatchDcd sends ev to every subscriber. Non-blocking: a slow
// subscriber's event is dropped and the drop is accounted for in the
// publisher rather than stalling the modem session goroutine.
func (b *Bridge) dispatchDcd(ev *pb.DcdChange) { b.dcd.Publish(ev) }

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

// errBridgeStopped is returned to any caller whose request/response dispatch
// channel was closed because the supervisor exited.
var errBridgeStopped = errors.New("modembridge: bridge stopped")

// closePendingRequests closes every reply channel in the three dispatchers
// (enum/tone/scan). Callers blocked in their per-call select see a
// zero-value response on the channel and return errBridgeStopped without
// waiting for their 5s / 30s per-call timeout. New registrations that
// race past the StateRunning fast-path check see a closed dispatcher
// and receive a closed channel immediately, closing the TOCTOU window
// between "caller reads state RUNNING" and "caller registers with the
// dispatcher".
//
// Must only be invoked from supervise()'s defer chain: at that point the
// session goroutine has already returned, so no Deliver is in flight and
// double-close is impossible.
func (b *Bridge) closePendingRequests() {
	b.enumDispatcher.Close()
	b.toneDispatcher.Close()
	b.scanDispatcher.Close()
}

// supervise is the top-level loop: spawn the child, drive one session, back
// off on error, repeat until the context is cancelled.
func (b *Bridge) supervise(ctx context.Context) {
	// Reset the dispatchers in case a previous supervise run closed them.
	// Callers that register a request before we transition to
	// StateRunning will either see StateStopped/StateStarting on their
	// fast-path check and bail, or see an open dispatcher here and
	// proceed normally.
	b.enumDispatcher.Reset()
	b.toneDispatcher.Reset()
	b.scanDispatcher.Reset()
	// Drop any cached channel stats or device levels from a prior session
	// so a restarted modem child starts with an empty view rather than
	// stale counters.
	b.status.Reset()

	defer close(b.done)
	defer close(b.frames)
	defer b.dcd.Close()
	defer b.closePendingRequests()

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
	listenAddr := modemListenAddr(b.cfg.SocketDir)
	cleanupListenAddr(listenAddr)

	args := modemExtraArgs(listenAddr)
	cmd := exec.CommandContext(ctx, b.cfg.BinaryPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", b.cfg.BinaryPath, err)
	}
	b.logger.Info("spawned modem", "pid", cmd.Process.Pid, "addr", listenAddr)
	if b.cfg.Metrics != nil {
		b.cfg.Metrics.SetChildUp(true)
	}

	// stdoutDone is closed by the scanner goroutine when it sees EOF
	// (the child's stdout pipe closed because the child exited) or an
	// error. The defer below waits on it after terminating the child
	// and before cmd.Wait so the scanner finishes its reads cleanly.
	var stdoutDone chan struct{}

	defer func() {
		terminateChild(cmd.Process)
		if stdoutDone != nil {
			<-stdoutDone
		}
		_ = cmd.Wait()
		cleanupListenAddr(listenAddr)
	}()

	// Readiness handshake: blocks until the Rust child signals it is
	// accepting connections. Returns the address to dial (on Unix this is
	// the socket path we already know; on Windows it is parsed from stdout).
	dialAddr, err := readDialAddr(stdout, b.cfg.ReadinessTimeout, listenAddr)
	if err != nil {
		return fmt.Errorf("readiness: %w", err)
	}
	// Drain stdout into the bounded ring buffer so the child's final
	// output is available for crash diagnostics and so this reader
	// goroutine cannot accumulate across restart storms.
	stdoutDone = make(chan struct{})
	go b.scanModemStdout(stdout, stdoutDone)

	conn, err := dialModem(dialAddr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", dialAddr, err)
	}
	defer conn.Close()

	return b.runSession(ctx, conn)
}

// LastModemStdout returns a snapshot of the last stdoutRingMax lines the
// modem child wrote to stdout. Useful for including the child's final
// output in crash diagnostics.
func (b *Bridge) LastModemStdout() []string {
	b.stdoutMu.Lock()
	defer b.stdoutMu.Unlock()
	out := make([]string, len(b.stdoutRing))
	copy(out, b.stdoutRing)
	return out
}

// appendStdoutLine adds line to the ring buffer, evicting the oldest
// entry if the buffer is full.
func (b *Bridge) appendStdoutLine(line string) {
	b.stdoutMu.Lock()
	if len(b.stdoutRing) >= stdoutRingMax {
		// Shift left by one. The ring is small (16) so this is cheap.
		copy(b.stdoutRing, b.stdoutRing[1:])
		b.stdoutRing = b.stdoutRing[:stdoutRingMax-1]
	}
	b.stdoutRing = append(b.stdoutRing, line)
	b.stdoutMu.Unlock()
}

// scanModemStdout reads newline-terminated lines from r into the ring
// buffer until r yields EOF or an error, then closes done to signal
// runOnce that the reader goroutine has exited.
func (b *Bridge) scanModemStdout(r io.Reader, done chan struct{}) {
	defer close(done)
	scanner := bufio.NewScanner(r)
	// Bump the max line size so a long Rust panic line does not
	// truncate the ring's view of the final output.
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	for scanner.Scan() {
		b.appendStdoutLine(scanner.Text())
	}
}

// GetChannelStats returns cached stats for a single channel.
func (b *Bridge) GetChannelStats(channel uint32) (*ChannelStats, bool) {
	return b.status.Channel(channel)
}

// GetAllChannelStats returns cached stats for all channels.
func (b *Bridge) GetAllChannelStats() map[uint32]*ChannelStats {
	return b.status.AllChannels()
}

// updateStatusCache stores the latest StatusUpdate for a channel.
func (b *Bridge) updateStatusCache(s *pb.StatusUpdate) { b.status.UpdateStatus(s) }

// ReconfigureAudioDevice performs a hot-swap of an audio device's configuration.
// It stops all audio, re-reads the full config from the database, and restarts.
// This handles both updates and deletes correctly.
func (b *Bridge) ReconfigureAudioDevice(ctx context.Context, _ uint32) error {
	return b.ReloadConfiguration(ctx)
}

// ReloadConfiguration stops all modem audio processing, re-reads the full
// configuration from the database, and restarts. Safe to call after deletes.
func (b *Bridge) ReloadConfiguration(ctx context.Context) error {
	if b.State() != StateRunning {
		return errors.New("modembridge: not in RUNNING state")
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

	// Re-push whatever is currently in the database.
	configured, err := b.pushConfiguration(b.sendIPC)
	if err != nil {
		return fmt.Errorf("push configuration: %w", err)
	}

	if configured {
		if err := b.sendIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StartAudio{StartAudio: &pb.StartAudio{}}}); err != nil {
			return fmt.Errorf("send StartAudio: %w", err)
		}
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

	reqID, ch := b.enumDispatcher.Register()
	defer b.enumDispatcher.Cancel(reqID)

	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_EnumerateAudioDevices{
		EnumerateAudioDevices: &pb.EnumerateAudioDevices{
			RequestId:     reqID,
			IncludeOutput: true,
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
		if resp == nil {
			return nil, errBridgeStopped
		}
		return convertDeviceList(resp), nil
	case <-timer.C:
		return nil, errors.New("modembridge: enumerate timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// dispatchEnumResponse delivers an AudioDeviceList to the waiting caller.
func (b *Bridge) dispatchEnumResponse(list *pb.AudioDeviceList) {
	b.enumDispatcher.Deliver(list.RequestId, list)
}

// InputLevel holds the level scan result for a single input device.
type InputLevel struct {
	Name      string  `json:"name"`
	PeakDBFS  float32 `json:"peak_dbfs"`
	HasSignal bool    `json:"has_signal"`
	Error     string  `json:"error,omitempty"`
}

// ScanInputLevels asks the Rust modem to briefly open each input device,
// measure peak levels, and return the results.
func (b *Bridge) ScanInputLevels(ctx context.Context) ([]InputLevel, error) {
	if b.State() != StateRunning {
		return nil, errors.New("modembridge: not in RUNNING state")
	}

	reqID, ch := b.scanDispatcher.Register()
	defer b.scanDispatcher.Cancel(reqID)

	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_ScanInputLevels{
		ScanInputLevels: &pb.ScanInputLevels{
			RequestId:  reqID,
			DurationMs: 500,
		},
	}}
	if err := b.sendIPC(msg); err != nil {
		return nil, fmt.Errorf("send ScanInputLevels: %w", err)
	}

	timer := time.NewTimer(30 * time.Second) // scanning many devices takes time
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp == nil {
			return nil, errBridgeStopped
		}
		return convertScanResult(resp), nil
	case <-timer.C:
		return nil, errors.New("modembridge: scan timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func convertScanResult(r *pb.InputLevelScanResult) []InputLevel {
	out := make([]InputLevel, 0, len(r.Devices))
	for _, d := range r.Devices {
		out = append(out, InputLevel{
			Name:      d.Name,
			PeakDBFS:  d.PeakDbfs,
			HasSignal: d.HasSignal,
			Error:     d.Error,
		})
	}
	return out
}

func (b *Bridge) dispatchScanResponse(r *pb.InputLevelScanResult) {
	b.scanDispatcher.Deliver(r.RequestId, r)
}

// isUsableAudioDevice filters out ALSA virtual devices that aren't useful
// for APRS (surround, HDMI, S/PDIF, dmix, etc.).
func isUsableAudioDevice(name string) bool {
	prefix, _, _ := strings.Cut(name, ":")
	switch prefix {
	case "surround21", "surround40", "surround41", "surround50", "surround51", "surround71",
		"hdmi", "iec958", "dmix", "dsnoop", "null":
		return false
	}
	return true
}

func convertDeviceList(list *pb.AudioDeviceList) []AvailableDevice {
	out := make([]AvailableDevice, 0, len(list.Devices))
	for _, d := range list.Devices {
		if !isUsableAudioDevice(d.Name) {
			continue
		}
		out = append(out, AvailableDevice{
			Name:        d.Name,
			Description: d.Description,
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

	reqID, ch := b.toneDispatcher.Register()
	defer b.toneDispatcher.Cancel(reqID)

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
		if resp == nil {
			return errBridgeStopped
		}
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
	b.toneDispatcher.Deliver(r.RequestId, r)
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
	b.status.UpdateDeviceLevel(u)
}

// GetAllDeviceLevels returns the latest cached audio levels for all devices.
func (b *Bridge) GetAllDeviceLevels() map[uint32]*DeviceLevel {
	return b.status.AllDeviceLevels()
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
	b.status.InjectStatsForTest(&ChannelStats{
		Channel:         channel,
		RxFrames:        rxFrames,
		RxBadFCS:        rxBadFCS,
		TxFrames:        txFrames,
		AudioLevelMark:  markLevel,
		AudioLevelSpace: spaceLevel,
		AudioLevelPeak:  peakLevel,
		DcdState:        dcd,
	})
}
