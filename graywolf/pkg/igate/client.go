package igate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// APRS-IS keepalive parameters. After idleKeepalive with no traffic we
// send a "# keepalive" comment; if there's no response within
// keepaliveTimeout we force a reconnect.
const (
	idleKeepalive    = 5 * time.Minute
	keepaliveTimeout = 30 * time.Second
	dialTimeout      = 10 * time.Second
)

// connState is set atomically under Igate.mu and reported to metrics.
type connState int

const (
	stateDisconnected connState = iota
	stateConnected
)

// lineSender is the minimal write side of an APRS-IS connection,
// broken out so tests can intercept sends without opening a TCP socket.
type lineSender interface {
	WriteLine(string) error
}

// client manages one APRS-IS TCP session: dial, login, read loop,
// keepalive monitor. It is owned by Igate and runs under its context.
type client struct {
	cfg    Config
	logger *slog.Logger

	mu   sync.Mutex
	conn net.Conn
	last time.Time // last time we received any byte from the server

	// handlers are invoked from the read loop.
	onLine      func(line string)
	onConnected func()
	onLost      func()
}

func newClient(cfg Config, logger *slog.Logger, onLine func(string), onConnected, onLost func()) *client {
	return &client{
		cfg:         cfg,
		logger:      logger,
		onLine:      onLine,
		onConnected: onConnected,
		onLost:      onLost,
	}
}

// closeConn closes the current APRS-IS connection, causing the read
// loop to exit and the supervisor to reconnect with current config.
func (c *client) closeConn() {
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.mu.Unlock()
}

// run executes one dial+login+read cycle. Returns when the session ends
// (error, disconnect, or ctx cancel). The caller re-dials with backoff.
func (c *client) run(ctx context.Context) error {
	addr := c.cfg.Server
	if addr == "" {
		return errors.New("igate: empty server address")
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	c.mu.Lock()
	c.conn = conn
	c.last = time.Now()
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()
	}()

	// Login handshake.
	login := buildLogin(c.cfg.Callsign, c.cfg.Passcode, c.cfg.SoftwareName, c.cfg.SoftwareVersion, c.cfg.ServerFilter)
	if err := c.writeLineLocked(login); err != nil {
		return fmt.Errorf("write login: %w", err)
	}
	// APRS-IS servers reply with one or more "# ..." comment lines; at
	// least one will contain "logresp CALL verified" or
	// "logresp CALL unverified". We require the verified form when a
	// non-"-1" passcode is configured; transmit-capable iGates must be
	// verified. Read lines up to a short deadline.
	reader := bufio.NewReader(conn)
	if err := awaitLogin(reader, c.cfg.Callsign, c.cfg.Passcode); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	c.mu.Lock()
	c.last = time.Now()
	c.mu.Unlock()
	if c.onConnected != nil {
		c.onConnected()
	}

	// Spawn the keepalive watchdog and a context watcher that closes
	// the conn on cancellation so the blocking ReadString in the read
	// loop below returns promptly.
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.keepalive(watchCtx)
	go func() {
		<-watchCtx.Done()
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.mu.Unlock()
	}()

	// Read loop: push each non-comment line to onLine, bump last on
	// every byte (including comments). A partial auth drop manifests
	// as an EOF or a reset comment; both surface as read errors.
	for {
		if ctx.Err() != nil {
			return nil
		}
		_ = conn.SetReadDeadline(time.Now().Add(idleKeepalive + keepaliveTimeout + 10*time.Second))
		line, err := reader.ReadString('\n')
		if err != nil {
			if c.onLost != nil {
				c.onLost()
			}
			if errors.Is(err, io.EOF) {
				return errors.New("aprs-is connection closed by server")
			}
			return fmt.Errorf("read: %w", err)
		}
		c.mu.Lock()
		c.last = time.Now()
		c.mu.Unlock()
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			// Server comment / keepalive response. If we observe a
			// "logresp ... unverified" *after* login succeeded, treat
			// it as a partial auth drop and force reconnect.
			if isUnverifiedLogResp(trimmed, c.cfg.Callsign) {
				if c.onLost != nil {
					c.onLost()
				}
				return errors.New("aprs-is partial auth drop: server re-sent unverified logresp")
			}
			continue
		}
		if c.onLine != nil {
			c.onLine(trimmed)
		}
	}
}

// keepalive sends a '#' comment when the connection has been idle for
// idleKeepalive, and forces a reconnect (by closing the conn) if the
// server does not respond within keepaliveTimeout.
func (c *client) keepalive(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	var awaitingResponse time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			c.mu.Lock()
			last := c.last
			conn := c.conn
			c.mu.Unlock()
			if conn == nil {
				return
			}
			idle := now.Sub(last)
			if !awaitingResponse.IsZero() {
				if last.After(awaitingResponse) {
					awaitingResponse = time.Time{}
				} else if now.Sub(awaitingResponse) >= keepaliveTimeout {
					c.logger.Warn("aprs-is keepalive timeout; forcing reconnect")
					_ = conn.Close()
					return
				}
				continue
			}
			if idle >= idleKeepalive {
				if err := c.writeLineLocked("# graywolf keepalive"); err != nil {
					c.logger.Warn("aprs-is keepalive write failed", "err", err)
					_ = conn.Close()
					return
				}
				awaitingResponse = now
			}
		}
	}
}

// WriteLine sends a CRLF-terminated line. Safe for concurrent callers.
func (c *client) WriteLine(s string) error {
	return c.writeLineLocked(s)
}

func (c *client) writeLineLocked(s string) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("aprs-is not connected")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := conn.Write([]byte(s + "\r\n"))
	return err
}

// buildLogin constructs the "user CALL pass NNN vers NAME VER filter F"
// login string per the APRS-IS spec. If no filter is configured, a
// no-match filter is sent so that the server does not forward any packets.
func buildLogin(call, pass, name, vers, filter string) string {
	if name == "" {
		name = "graywolf"
	}
	if vers == "" {
		vers = "0.1"
	}
	if pass == "" {
		pass = "-1"
	}
	if filter == "" {
		filter = "r/-48.87/-27.14/0" // 0 km range in the south Atlantic — matches nothing
	}
	s := fmt.Sprintf("user %s pass %s vers %s %s filter %s", call, pass, name, vers, filter)
	return s
}

// awaitLogin reads server lines until a logresp is seen or a short
// deadline expires. Returns an error if the server reports unverified
// while a non-"-1" passcode was configured (transmit would be refused).
func awaitLogin(r *bufio.Reader, call, pass string) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		line, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "#") {
			// Premature data line; treat as success, servers often
			// start streaming before a formal logresp.
			return nil
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "logresp") {
			if strings.Contains(lower, "unverified") && pass != "" && pass != "-1" {
				return errors.New("server reported unverified login")
			}
			return nil
		}
	}
	return errors.New("timeout waiting for logresp")
}

// isUnverifiedLogResp flags a post-login "logresp" comment from the
// server that downgrades this session to unverified (APRS-IS servers
// occasionally re-emit this on auth drop). Case-insensitive.
func isUnverifiedLogResp(line, call string) bool {
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "logresp") {
		return false
	}
	if call != "" && !strings.Contains(lower, strings.ToLower(call)) {
		return false
	}
	return strings.Contains(lower, "unverified")
}
