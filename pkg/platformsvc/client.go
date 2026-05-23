//go:build android

package platformsvc

import (
	"context"
	"io"
)

// Client is the typed Go API consumed by pkg/gps/android.go,
// pkg/pttdevice/android.go, and the modembridge PTT relay (phases 4-5).
// Implementations connect to the Kotlin PlatformServer over UDS.
type Client interface {
	// ConnectWithReconnect dials the UDS and, on disconnect, re-dials with
	// exponential backoff until ctx is cancelled or Close() is called.
	// This is the production entry point.
	ConnectWithReconnect(ctx context.Context) error
	Hello(ctx context.Context, schemaVersion uint32) (*HelloResponse, error)
	SubscribeGpsFix(ctx context.Context, ch chan<- *GpsFix) error
	SubscribeGnssStatus(ctx context.Context, ch chan<- *GnssStatusUpdate) error
	SubscribeAudioRouteChanged(ctx context.Context, ch chan<- *AudioRouteChanged) error
	ListUsbDevices(ctx context.Context, class UsbClass) ([]*UsbDevice, error)
	SelectUsbDevice(ctx context.Context, vid, pid uint16) (*UsbHandle, error)
	KeyPtt(ctx context.Context, method PttMethod, handle *UsbHandle) (*PttAck, error)
	UnkeyPtt(ctx context.Context, method PttMethod, handle *UsbHandle) (*PttAck, error)
	// BondedBtDevices enumerates the currently-bonded Bluetooth devices on
	// the Android side. One-shot request/response; safe to call repeatedly.
	BondedBtDevices(ctx context.Context) ([]BondedBtDevice, error)
	// BtSerialOpen opens an RFCOMM SPP stream to the bonded device at mac
	// and returns a multiplexed io.ReadWriteCloser. Multiple handles may
	// be open concurrently; each is routed independently by the dispatch
	// layer. Close on the returned handle sends a final SerialClose to the
	// server and unregisters the handle.
	BtSerialOpen(ctx context.Context, mac string) (io.ReadWriteCloser, error)
	// AvailableUsbSerialDevices enumerates attached serial-capable USB
	// devices on the Android side. One-shot request/response.
	AvailableUsbSerialDevices(ctx context.Context) ([]UsbSerialDevice, error)
	// UsbSerialOpen opens a serial stream to the attached USB device matching
	// vidPid ("vid:pid" hex) at baud, returning a multiplexed
	// io.ReadWriteCloser routed independently by the dispatch layer.
	UsbSerialOpen(ctx context.Context, vidPid string, baud uint32) (io.ReadWriteCloser, error)
	Close() error
}

// NewClient returns an unconnected Client. Call ConnectWithReconnect to
// dial the UDS and engage the reconnect loop, then Hello to handshake.
// Close terminates the reconnect loop. The one-shot Connect path stays
// internal (test-only).
func NewClient(socketPath string) Client {
	return newClient(socketPath)
}
