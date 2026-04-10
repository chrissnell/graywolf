//go:build !windows

package modembridge

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// modemListenAddr returns the Unix socket path for the modem IPC.
func modemListenAddr(socketDir string) string {
	return filepath.Join(socketDir, fmt.Sprintf("graywolf-modem-%d.sock", os.Getpid()))
}

// modemExtraArgs returns CLI args that tell the modem where to listen.
func modemExtraArgs(listenAddr string) []string {
	return []string{listenAddr}
}

// cleanupListenAddr removes a stale socket file.
func cleanupListenAddr(addr string) {
	_ = os.Remove(addr)
}

// readDialAddr waits for the modem's readiness signal and returns the
// address to dial. On Unix the address is the socket path we already know;
// the readiness signal is a single '\n' byte.
func readDialAddr(r io.Reader, timeout time.Duration, listenAddr string) (string, error) {
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
			return "", res.err
		}
		if res.b != '\n' {
			return "", fmt.Errorf("unexpected readiness byte %q", res.b)
		}
		return listenAddr, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout after %s", timeout)
	}
}

// dialModem connects to the modem's Unix domain socket.
func dialModem(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", addr, timeout)
}

// terminateChild sends SIGTERM to the child process.
func terminateChild(p *os.Process) {
	_ = p.Signal(syscall.SIGTERM)
}
