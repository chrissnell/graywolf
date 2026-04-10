package modembridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/metrics"
)

// supervisorConfig carries the tuning knobs and callback the supervisor
// needs to run the child process lifecycle.
type supervisorConfig struct {
	BinaryPath       string
	SocketDir        string
	ReadinessTimeout time.Duration
	MaxBackoff       time.Duration

	// Metrics is optional. If non-nil, supervisor increments ChildRestarts
	// and toggles ChildUp across each session.
	Metrics *metrics.Metrics

	// RunSession is called once per successfully-dialed child connection.
	// It returns when the child disconnects, errors, or the supplied
	// context is cancelled. The supervisor blocks inside this call for
	// the duration of each session, then loops to restart.
	RunSession func(ctx context.Context, conn net.Conn) error

	// OnStateChange is called whenever the supervisor transitions
	// between states. It must be non-blocking.
	OnStateChange func(state State)
}

// supervisor owns the child-process lifecycle: spawning, readiness
// handshake, dial, session execution via RunSession, crash recovery with
// exponential backoff, and capture of the child's final stdout for
// crash diagnostics.
type supervisor struct {
	cfg    supervisorConfig
	logger *slog.Logger

	mu    sync.Mutex
	state State

	stdoutMu   sync.Mutex
	stdoutRing []string
}

// stdoutRingMax is the maximum number of lines retained in the stdout
// ring buffer. Sized to capture a typical Rust panic trace plus a few
// surrounding log lines.
const stdoutRingMax = 16

func newSupervisor(cfg supervisorConfig, logger *slog.Logger) *supervisor {
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &supervisor{
		cfg:    cfg,
		logger: logger,
		state:  StateStopped,
	}
}

// State returns the current supervisor state.
func (s *supervisor) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// setState stores the new state, logs it, and notifies the OnStateChange
// callback if one is installed.
func (s *supervisor) setState(st State) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
	s.logger.Info("modembridge state", "state", st.String())
	if s.cfg.OnStateChange != nil {
		s.cfg.OnStateChange(st)
	}
}

// LastStdout returns a snapshot of the last stdoutRingMax lines the
// modem child wrote to stdout. Useful for including the child's final
// output in crash diagnostics.
func (s *supervisor) LastStdout() []string {
	s.stdoutMu.Lock()
	defer s.stdoutMu.Unlock()
	out := make([]string, len(s.stdoutRing))
	copy(out, s.stdoutRing)
	return out
}

func (s *supervisor) appendStdoutLine(line string) {
	s.stdoutMu.Lock()
	if len(s.stdoutRing) >= stdoutRingMax {
		copy(s.stdoutRing, s.stdoutRing[1:])
		s.stdoutRing = s.stdoutRing[:stdoutRingMax-1]
	}
	s.stdoutRing = append(s.stdoutRing, line)
	s.stdoutMu.Unlock()
}

// scanModemStdout reads newline-terminated lines from r into the ring
// buffer until r yields EOF or an error, then closes done to signal the
// caller that the reader goroutine has exited.
func (s *supervisor) scanModemStdout(r io.Reader, done chan struct{}) {
	defer close(done)
	scanner := bufio.NewScanner(r)
	// Bump the max line size so a long Rust panic line does not
	// truncate the ring's view of the final output.
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	for scanner.Scan() {
		s.appendStdoutLine(scanner.Text())
	}
}

// Run is the top-level supervisor loop: spawn the child, drive one
// session through RunSession, back off on error, repeat until ctx is
// cancelled. It returns when ctx is cancelled.
func (s *supervisor) Run(ctx context.Context) {
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			s.setState(StateStopped)
			return
		}
		s.setState(StateStarting)

		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			s.setState(StateStopped)
			return
		}
		if err != nil {
			s.logger.Error("modembridge session ended", "err", err)
		}
		if s.cfg.Metrics != nil {
			s.cfg.Metrics.ChildRestarts.Inc()
			s.cfg.Metrics.SetChildUp(false)
		}

		s.setState(StateRestarting)
		select {
		case <-ctx.Done():
			s.setState(StateStopped)
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > s.cfg.MaxBackoff {
			backoff = s.cfg.MaxBackoff
		}
	}
}

// runOnce brings the child up, invokes RunSession for one session, and
// tears the child down. It returns whatever error caused the session to
// end (or nil on clean shutdown via context cancel).
func (s *supervisor) runOnce(ctx context.Context) error {
	listenAddr := modemListenAddr(s.cfg.SocketDir)
	cleanupListenAddr(listenAddr)

	args := modemExtraArgs(listenAddr)
	cmd := exec.CommandContext(ctx, s.cfg.BinaryPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", s.cfg.BinaryPath, err)
	}
	s.logger.Info("spawned modem", "pid", cmd.Process.Pid, "addr", listenAddr)
	if s.cfg.Metrics != nil {
		s.cfg.Metrics.SetChildUp(true)
	}

	// stdoutDone is closed by the scanner goroutine when it sees EOF or
	// an error. The defer below waits on it after terminating the child
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
	// accepting connections. Returns the address to dial (on Unix this
	// is the socket path we already know; on Windows it is parsed from
	// stdout).
	dialAddr, err := readDialAddr(stdout, s.cfg.ReadinessTimeout, listenAddr)
	if err != nil {
		return fmt.Errorf("readiness: %w", err)
	}
	// Drain stdout into the bounded ring buffer so the child's final
	// output is available for crash diagnostics and so this reader
	// goroutine cannot accumulate across restart storms.
	stdoutDone = make(chan struct{})
	go s.scanModemStdout(stdout, stdoutDone)

	conn, err := dialModem(dialAddr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", dialAddr, err)
	}
	defer conn.Close()

	return s.cfg.RunSession(ctx, conn)
}
