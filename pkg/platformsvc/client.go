//go:build android

package platformsvc

import "context"

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
	SubscribeAudioRouteChanged(ctx context.Context, ch chan<- *AudioRouteChanged) error
	ListUsbDevices(ctx context.Context, class UsbClass) ([]*UsbDevice, error)
	SelectUsbDevice(ctx context.Context, vid, pid uint16) (*UsbHandle, error)
	KeyPtt(ctx context.Context, method PttMethod, handle *UsbHandle) (*PttAck, error)
	UnkeyPtt(ctx context.Context, method PttMethod, handle *UsbHandle) (*PttAck, error)
	Close() error
}

// NewClient returns an unconnected Client. Call ConnectWithReconnect to
// dial the UDS and engage the reconnect loop, then Hello to handshake.
// Close terminates the reconnect loop. The one-shot Connect path stays
// internal (test-only).
func NewClient(socketPath string) Client {
	return newClient(socketPath)
}
