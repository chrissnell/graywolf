//go:build !linux

package tcpstall

import (
	"net"
	"time"
)

// Set is only implemented on Linux (TCP_USER_TIMEOUT).
func Set(net.Conn, time.Duration) error { return nil }
