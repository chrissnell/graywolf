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
	Store *configstore.Store
	// Metrics receives status updates and frame counts. Optional.
	Metrics *metrics.Metrics
	// Logger is used for structured logging. Defaults to slog.Default().
	Logger *slog.Logger
	// FrameBufferSize controls the capacity of the Frames() channel.
	FrameBufferSize int
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
}

// Bridge supervises the Rust modem child and exposes received frames to
// consumers.
type Bridge struct {
	cfg    Config
	logger *slog.Logger

	frames chan *pb.ReceivedFrame

	mu    sync.Mutex
	state State

	// Runtime fields guarded by mu.
	cancel context.CancelFunc
	done   chan struct{}
}

// New builds a bridge. Call Start to run it.
func New(cfg Config) *Bridge {
	cfg.applyDefaults()
	return &Bridge{
		cfg:    cfg,
		logger: cfg.Logger,
		frames: make(chan *pb.ReceivedFrame, cfg.FrameBufferSize),
		state:  StateStopped,
	}
}

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
