package modembridge

import (
	"log/slog"
	"os"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/metrics"
)

// Config drives a Bridge.
type Config struct {
	// BinaryPath is the path to graywolf-modem. Defaults to
	// "./target/release/graywolf-modem" (the workspace-shared cargo
	// output directory at the repo root). Ignored when ExistingSocket
	// is set.
	BinaryPath string
	// SocketDir is where the Unix socket file lives. Defaults to
	// os.TempDir(). Ignored when ExistingSocket is set.
	SocketDir string
	// ExistingSocket, when non-empty, switches the supervisor into
	// connect-only mode: it skips fork+exec and the readiness
	// handshake and dials this UDS path directly. Used on Android
	// where the modem cdylib is loaded in-process by the Kotlin
	// Service and exposed at a Service-allocated socket path. The
	// caller (Service) owns binary lifecycle and supervises restarts
	// at the OS process level; the in-Go supervisor still
	// reconnects on session-end so a transient socket close doesn't
	// kill the Go process unnecessarily.
	ExistingSocket string
	// ReadinessTimeout bounds the wait for the child's stdout readiness byte.
	// Ignored when ExistingSocket is set.
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
	// DcdBufferSize is retained for backwards compatibility but is not
	// currently consulted; dcdPublisher uses a fixed per-subscriber buffer.
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
