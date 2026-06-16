//go:build linux

package gps

import (
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// linuxBaudMap maps a plain baud rate to its termios speed constant. NMEA
// devices run at standard rates (4800/9600/38400 are the common ones), so
// the non-standard BOTHER path is intentionally not supported here.
var linuxBaudMap = map[int]uint32{
	1200:   unix.B1200,
	2400:   unix.B2400,
	4800:   unix.B4800,
	9600:   unix.B9600,
	19200:  unix.B19200,
	38400:  unix.B38400,
	57600:  unix.B57600,
	115200: unix.B115200,
	230400: unix.B230400,
}

// linuxNMEAPort is a minimal NMEA-RX serial port. Unlike go.bug.st/serial
// it never calls TIOCEXCL: holding the tty exclusively locks the
// graywolf-modem PTT driver out of the same device on shared serial rigs
// (Digirig: NMEA on RX, PTT on RTS), so PTT silently failed whenever the
// GPS reader won the startup race (GRA-118). Reads use VMIN=0/VTIME so a
// read returns (0, nil) on timeout, exactly what the NMEA scanner's
// timeoutReader expects, and a self-pipe wakes a blocked read on Close
// without the fd-reuse hazard of closing the fd out from under it.
type linuxNMEAPort struct {
	fd        int
	wakeR     int
	wakeW     int
	timeoutMs int
	closeOnce sync.Once
}

func openNMEASerial(device string, baud int) (nmeaPort, error) {
	speed, ok := linuxBaudMap[baud]
	if !ok {
		return nil, fmt.Errorf("unsupported baud rate %d", baud)
	}

	// O_NONBLOCK avoids the DCD-blocking open() seen on some USB-serial
	// adapters; we keep the fd non-blocking and gate reads with poll().
	fd, err := unix.Open(device, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}

	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("tcgetattr: %w", err)
	}

	// Raw mode, 8N1, no flow control. CLOCAL ignores modem-control inputs
	// (RX must work without DCD); CREAD enables the receiver.
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF | unix.IXANY
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB | unix.CSTOPB | unix.CRTSCTS
	t.Cflag |= unix.CS8 | unix.CLOCAL | unix.CREAD

	for _, b := range linuxBaudMap {
		t.Cflag &^= b
	}
	t.Cflag |= speed
	t.Ispeed = speed
	t.Ospeed = speed

	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(fd, unix.TCSETS, t); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("tcsetattr: %w", err)
	}

	// Deassert RTS and DTR. The kernel raises both on open; on a Digirig
	// raised RTS keys PTT the instant GPS connects (#305). Hold them low
	// and leave RTS for the PTT driver to assert when it keys. Crucially,
	// no TIOCEXCL is issued, so the PTT driver can open the same tty.
	// ENOTTY means the device has no modem-control lines (a pty or a
	// modem-less virtual serial), so there's no RTS PTT to worry about
	// and the deassert is simply skipped.
	if bits, err := unix.IoctlGetInt(fd, unix.TIOCMGET); err == nil {
		bits &^= unix.TIOCM_RTS | unix.TIOCM_DTR
		if err := unix.IoctlSetPointerInt(fd, unix.TIOCMSET, bits); err != nil && err != unix.ENOTTY {
			unix.Close(fd)
			return nil, fmt.Errorf("TIOCMSET: %w", err)
		}
	} else if err != unix.ENOTTY {
		unix.Close(fd)
		return nil, fmt.Errorf("TIOCMGET: %w", err)
	}

	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("wake pipe: %w", err)
	}

	return &linuxNMEAPort{
		fd:        fd,
		wakeR:     pipe[0],
		wakeW:     pipe[1],
		timeoutMs: 500,
	}, nil
}

func (p *linuxNMEAPort) Read(b []byte) (int, error) {
	for {
		pfds := []unix.PollFd{
			{Fd: int32(p.fd), Events: unix.POLLIN},
			{Fd: int32(p.wakeR), Events: unix.POLLIN},
		}
		_, err := unix.Poll(pfds, p.timeoutMs)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		// Close() wrote to the wake pipe, or the device fd went away.
		if pfds[1].Revents != 0 {
			return 0, io.EOF
		}
		if pfds[0].Revents&int16(unix.POLLNVAL|unix.POLLERR|unix.POLLHUP) != 0 {
			return 0, io.EOF
		}
		if pfds[0].Revents&int16(unix.POLLIN) == 0 {
			// Poll timeout: report a (0, nil) read so timeoutReader can
			// observe ctx and loop.
			return 0, nil
		}
		n, err := unix.Read(p.fd, b)
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			// poll() reported the device readable but read drained zero
			// bytes: end-of-stream (e.g. the adapter was unplugged and the
			// disconnect surfaced as readable-EOF rather than POLLHUP).
			// Surface it as EOF so the reader's retry-with-backoff layer
			// re-opens the port instead of looping on (0, nil) forever.
			return 0, io.EOF
		}
		return n, nil
	}
}

func (p *linuxNMEAPort) SetReadTimeout(d time.Duration) error {
	ms := d.Milliseconds()
	switch {
	case d < 0:
		p.timeoutMs = -1 // block indefinitely
	case ms < 1:
		p.timeoutMs = 1
	default:
		p.timeoutMs = int(ms)
	}
	return nil
}

func (p *linuxNMEAPort) Close() error {
	var err error
	p.closeOnce.Do(func() {
		// Wake a blocked Read before closing the device fd so it never
		// reads from a recycled fd number.
		_, _ = unix.Write(p.wakeW, []byte{0})
		err = unix.Close(p.fd)
		unix.Close(p.wakeR)
		unix.Close(p.wakeW)
	})
	return err
}
