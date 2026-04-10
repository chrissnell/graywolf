//go:build windows

package modembridge

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// modemListenAddr returns a placeholder — on Windows the modem picks its
// own TCP port and reports it via stdout.
func modemListenAddr(_ string) string {
	return ""
}

// modemExtraArgs returns no extra CLI args; the Windows modem binary binds
// TCP without needing a path argument.
func modemExtraArgs(_ string) []string {
	return nil
}

// cleanupListenAddr is a no-op on Windows (TCP sockets leave no files).
func cleanupListenAddr(_ string) {}

// readDialAddr waits for the modem's readiness signal and returns the TCP
// address to dial. The modem writes "<port>\n" to stdout.
func readDialAddr(r io.Reader, timeout time.Duration, _ string) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		br := bufio.NewReader(r)
		line, err := br.ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			return "", res.err
		}
		portStr := strings.TrimSpace(res.line)
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return "", fmt.Errorf("invalid port in readiness line %q: %w", portStr, err)
		}
		return fmt.Sprintf("127.0.0.1:%d", port), nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout after %s", timeout)
	}
}

// dialModem connects to the modem's TCP listener on localhost.
func dialModem(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, timeout)
}

// terminateChild kills the child process. Windows does not support SIGTERM.
func terminateChild(p *os.Process) {
	_ = p.Kill()
}
