package kiss

import (
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClassicBtDevice is a paired Bluetooth Classic device. MAC is the
// colon-separated uppercase address.
type ClassicBtDevice struct {
	MAC  string
	Name string
}

// classicBtWriteTimeout bounds a single RFCOMM write.
const classicBtWriteTimeout = 10 * time.Second

var btAddrRe = regexp.MustCompile(`^([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}(#[0-9]{1,2})?$`)

// ParseBluetoothAddress recognises a Bluetooth Classic device address,
// optionally with an RFCOMM channel override ("AA:BB:CC:DD:EE:FF#3"; 0
// means look the Serial Port channel up via SDP). It returns the address
// in colon-separated uppercase form.
func ParseBluetoothAddress(device string) (addr string, channel int, ok bool) {
	if !btAddrRe.MatchString(device) {
		return "", 0, false
	}
	if i := strings.IndexByte(device, '#'); i >= 0 {
		ch, err := strconv.Atoi(device[i+1:])
		if err != nil || ch < 1 || ch > 30 {
			return "", 0, false
		}
		channel = ch
		device = device[:i]
	}
	return strings.ToUpper(strings.ReplaceAll(device, "-", ":")), channel, true
}

// nativeBluetoothDevice reports whether device will be opened through the
// native macOS Bluetooth transport rather than a tty.
func nativeBluetoothDevice(device string) bool {
	_, _, ok := ParseBluetoothAddress(device)
	return ok && classicBluetoothNative
}

// btConn is the io.ReadWriteCloser over a native RFCOMM channel. Received
// bytes arrive on r (a pipe the native layer fills and closes when the link
// drops, giving Read an EOF); writes and teardown go through the callbacks.
type btConn struct {
	r       *os.File
	write   func([]byte) error
	closeFn func()

	once sync.Once
}

func (c *btConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *btConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := c.write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *btConn) Close() error {
	c.once.Do(func() {
		c.closeFn()
		_ = c.r.Close()
	})
	return nil
}

var errClassicBtUnsupported = errors.New(
	"kiss: Bluetooth Classic by address is only available on macOS builds with cgo")

var _ io.ReadWriteCloser = (*btConn)(nil)
