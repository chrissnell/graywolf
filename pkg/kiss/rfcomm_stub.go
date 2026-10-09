//go:build !(darwin && cgo)

package kiss

import "io"

const classicBluetoothNative = false

// ClassicBluetoothSupported reports whether OpenClassicBluetooth and
// PairedClassicBluetooth work in this build.
func ClassicBluetoothSupported() bool { return false }

// OpenClassicBluetooth is unavailable outside macOS cgo builds.
func OpenClassicBluetooth(string, int) (io.ReadWriteCloser, error) {
	return nil, errClassicBtUnsupported
}

// PairedClassicBluetooth is unavailable outside macOS cgo builds.
func PairedClassicBluetooth() ([]ClassicBtDevice, error) {
	return nil, errClassicBtUnsupported
}
