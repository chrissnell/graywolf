//go:build linux

package tcpstall

import (
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// Set makes the kernel abort the connection when sent data
// stays unacknowledged for longer than d. This also covers a TNC that keeps
// the connection open but stops reading it (zero receive window): writes
// still succeed into the socket buffer, so without it the client never
// notices and never reconnects.
func Set(conn net.Conn, d time.Duration) error {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return nil
	}
	raw, err := tc.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := raw.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, int(d/time.Millisecond))
	}); err != nil {
		return err
	}
	return serr
}
