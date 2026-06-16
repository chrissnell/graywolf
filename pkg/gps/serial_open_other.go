//go:build !linux

package gps

import "go.bug.st/serial"

// openNMEASerial opens device at baud for NMEA RX with RTS and DTR held
// low so a shared-line PTT driver isn't keyed by the GPS connection
// (issue #305). On non-Linux platforms go.bug.st/serial drives the port.
//
// Note: go.bug.st acquires exclusive access (TIOCEXCL) on Unix. The
// shared-serial PTT contention this would cause (GRA-118) is a Linux-only
// scenario (Raspberry Pi + Digirig), handled by the dedicated opener in
// serial_open_linux.go.
func openNMEASerial(device string, baud int) (nmeaPort, error) {
	return serial.Open(device, &serial.Mode{
		BaudRate:          baud,
		InitialStatusBits: &serial.ModemOutputBits{RTS: false, DTR: false},
	})
}
