//go:build !android && (linux || cgo)

// Package kiss — BLE transport for KISS TNCs (Mobilinkd, NUS-based radios).
//
// Connects directly to a BLE KISS TNC without requiring OS-level pairing.
// Two GATT profiles are supported:
//
//   - Mobilinkd (TNC3/TNC4): proprietary service UUID 00000001-ba2a-46c9-ae49-01b0961f68bb
//   - Nordic UART Service (NUS): standard 6E400001-B5A3-F393-E0A9-E50E24DCCA9E,
//     used by BTECH UV-PRO, VERO VR-N76, Radioddity GA-5WB, and others.
//
// The connection is opened via tinygo.org/x/bluetooth and exposed as an
// io.ReadWriteCloser so SerialSupervisor treats it identically to a serial port.
//
// Build constraints: requires CGO on macOS (CoreBluetooth); pure-Go
// on Linux (BlueZ via D-Bus). Build with CGO_ENABLED=0 on macOS will
// use ble_mobilinkd_stub.go instead.
package kiss

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// Mobilinkd BLE KISS GATT UUIDs. Fixed in Mobilinkd firmware; identical
// across TNC3, TNC4, and compatible firmware.
var (
	mobilinkdServiceUUID = mustParseUUID("00000001-ba2a-46c9-ae49-01b0961f68bb")
	mobilinkdTxUUID      = mustParseUUID("00000003-ba2a-46c9-ae49-01b0961f68bb") // TNC→host, notify
	mobilinkdRxUUID      = mustParseUUID("00000002-ba2a-46c9-ae49-01b0961f68bb") // host→TNC, write-without-response
)

// Nordic UART Service (NUS) GATT UUIDs. Standard profile used by BTECH
// UV-PRO, VERO VR-N76, Radioddity GA-5WB, and many other BLE radios.
var (
	nusServiceUUID = mustParseUUID("6E400001-B5A3-F393-E0A9-E50E24DCCA9E")
	nusTxUUID      = mustParseUUID("6E400003-B5A3-F393-E0A9-E50E24DCCA9E") // TNC→host, notify
	nusRxUUID      = mustParseUUID("6E400002-B5A3-F393-E0A9-E50E24DCCA9E") // host→TNC, write-without-response
)

func mustParseUUID(s string) bluetooth.UUID {
	u, err := bluetooth.ParseUUID(s)
	if err != nil {
		panic("bluetooth: invalid UUID literal: " + s)
	}
	return u
}

// adapterOnce ensures Enable() is called at most once per process lifetime.
var (
	adapterOnce sync.Once
	adapterErr  error
)

func enableAdapter() error {
	adapterOnce.Do(func() {
		adapterErr = bluetooth.DefaultAdapter.Enable()
	})
	return adapterErr
}

// BLEDevice is a discovered Mobilinkd BLE peripheral.
type BLEDevice struct {
	Addr string
	Name string
	RSSI int16
}

// ScanBLEMobilinkd scans for nearby BLE KISS TNC devices. It recognises
// both Mobilinkd peripherals (proprietary service UUID) and NUS-based
// devices such as the BTECH UV-PRO, VERO VR-N76, and Radioddity GA-5WB.
// It calls discovered for each unique peripheral found and returns when
// ctx is cancelled. The caller is responsible for setting a deadline on
// ctx to bound the scan duration.
func ScanBLEMobilinkd(ctx context.Context, discovered func(BLEDevice)) error {
	if err := enableAdapter(); err != nil {
		return fmt.Errorf("BLE: adapter enable: %w", err)
	}

	seen := make(map[string]bool)
	scanDone := make(chan error, 1)

	go func() {
		err := bluetooth.DefaultAdapter.Scan(func(_ *bluetooth.Adapter, result bluetooth.ScanResult) {
			if !result.HasServiceUUID(mobilinkdServiceUUID) && !result.HasServiceUUID(nusServiceUUID) {
				return
			}
			addr := result.Address.String()
			if seen[addr] {
				return
			}
			seen[addr] = true
			discovered(BLEDevice{
				Addr: addr,
				Name: result.LocalName(),
				RSSI: result.RSSI,
			})
		})
		scanDone <- err
	}()

	select {
	case <-ctx.Done():
		_ = bluetooth.DefaultAdapter.StopScan()
		<-scanDone
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// Timeout is the normal completion path; don't surface it as an error.
			return nil
		}
		return ctx.Err()
	case err := <-scanDone:
		return err
	}
}

// bleProfile holds the resolved service and GATT characteristic UUIDs
// for a connected BLE TNC.
type bleProfile struct {
	serviceUUID bluetooth.UUID
	txUUID      bluetooth.UUID // TNC→host, notify
	rxUUID      bluetooth.UUID // host→TNC, write-without-response
}

// knownProfiles lists recognised BLE KISS GATT profiles in preference order.
// Mobilinkd is tried first; NUS (BTECH UV-PRO, VERO VR-N76, Radioddity
// GA-5WB, etc.) is the fallback.
var knownProfiles = []bleProfile{
	{mobilinkdServiceUUID, mobilinkdTxUUID, mobilinkdRxUUID},
	{nusServiceUUID, nusTxUUID, nusRxUUID},
}

// OpenBLEMobilinkd connects to the BLE TNC at addr (a string produced
// by Address.String() on the scan result — a UUID on macOS, a MAC on Linux).
// It tries the Mobilinkd GATT profile first, then falls back to the Nordic
// UART Service (NUS) profile used by BTECH UV-PRO, VERO VR-N76, Radioddity
// GA-5WB, and others. Returns an io.ReadWriteCloser suitable for injection
// as SerialConfig.OpenFunc.
//
// The baud parameter is ignored (BLE is a framed byte stream with no
// host-side baud rate). Signature matches kiss.OpenFunc.
func OpenBLEMobilinkd(addr string, _ uint32) (io.ReadWriteCloser, error) {
	if addr == "" {
		return nil, errors.New("BLE: empty device address — scan and save the interface to set it")
	}
	if err := enableAdapter(); err != nil {
		return nil, fmt.Errorf("BLE: adapter enable: %w", err)
	}

	var bleAddr bluetooth.Address
	bleAddr.Set(addr)

	// Install the disconnect handler before Connect; the bluetooth package
	// requires this ordering for the callback to fire on a physical disconnect.
	physical := make(chan struct{})
	var physicalOnce sync.Once
	bluetooth.DefaultAdapter.SetConnectHandler(func(dev bluetooth.Device, connected bool) {
		if !connected && dev.Address.String() == bleAddr.String() {
			physicalOnce.Do(func() { close(physical) })
		}
	})

	// Connect in a goroutine with a hard timeout. On macOS/CoreBluetooth,
	// ConnectionParams.ConnectionTimeout is not honored; Connect() blocks
	// indefinitely after a sleep/wake cycle or when the TNC is unreachable.
	// connCh is buffered so the goroutine never leaks: any device that
	// arrives after the timeout is immediately disconnected.
	const bleConnectTimeout = 15 * time.Second
	type bleResult struct {
		device bluetooth.Device
		err    error
	}
	connCh := make(chan bleResult, 1)
	go func() {
		d, e := bluetooth.DefaultAdapter.Connect(bleAddr, bluetooth.ConnectionParams{
			ConnectionTimeout: bluetooth.NewDuration(bleConnectTimeout),
		})
		connCh <- bleResult{d, e}
	}()

	var device bluetooth.Device
	select {
	case r := <-connCh:
		if r.err != nil {
			bluetooth.DefaultAdapter.SetConnectHandler(func(bluetooth.Device, bool) {})
			return nil, fmt.Errorf("BLE: connect %s: %w", addr, r.err)
		}
		device = r.device
	case <-time.After(bleConnectTimeout):
		bluetooth.DefaultAdapter.SetConnectHandler(func(bluetooth.Device, bool) {})
		// Disconnect any device that arrives after our deadline.
		go func() {
			if r := <-connCh; r.err == nil {
				_ = r.device.Disconnect()
			}
		}()
		return nil, fmt.Errorf("BLE: connect %s: timed out after %s", addr, bleConnectTimeout)
	}

	// Try each known profile in order; use the first one whose service is present.
	var chosenTxUUID, chosenRxUUID bluetooth.UUID
	var svc bluetooth.DeviceService
	for _, p := range knownProfiles {
		svcs, err := device.DiscoverServices([]bluetooth.UUID{p.serviceUUID})
		if err != nil || len(svcs) == 0 {
			continue
		}
		svc = svcs[0]
		chosenTxUUID = p.txUUID
		chosenRxUUID = p.rxUUID
		break
	}
	if chosenTxUUID == (bluetooth.UUID{}) {
		_ = device.Disconnect()
		return nil, fmt.Errorf("BLE: no recognised KISS service found on %s (tried Mobilinkd, NUS)", addr)
	}

	chars, err := svc.DiscoverCharacteristics([]bluetooth.UUID{chosenTxUUID, chosenRxUUID})
	if err != nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("BLE: discover characteristics: %w", err)
	}

	var txChar, rxChar *bluetooth.DeviceCharacteristic
	for i := range chars {
		u := chars[i].UUID()
		switch u {
		case chosenTxUUID:
			c := chars[i]
			txChar = &c
		case chosenRxUUID:
			c := chars[i]
			rxChar = &c
		}
	}
	if txChar == nil || rxChar == nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("BLE: required KISS characteristics not found on %s (tx=%v rx=%v)", addr, txChar != nil, rxChar != nil)
	}

	conn := &bleMobilinkdConn{
		device:   device,
		rxChar:   rxChar,
		rxBuf:    make(chan []byte, 64),
		done:     make(chan struct{}),
		physical: physical,
	}

	if err := txChar.EnableNotifications(func(buf []byte) {
		// Copy buf: the underlying array may be reused after the callback returns.
		data := make([]byte, len(buf))
		copy(data, buf)
		select {
		case conn.rxBuf <- data:
		case <-conn.done:
		}
	}); err != nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("BLE: enable TX notifications: %w", err)
	}

	return conn, nil
}

// bleMobilinkdConn wraps a BLE GATT connection as io.ReadWriteCloser.
// Read delivers bytes from TNC TX notifications; Write sends bytes to
// TNC RX characteristic in MTU-sized chunks.
type bleMobilinkdConn struct {
	device    bluetooth.Device
	rxChar    *bluetooth.DeviceCharacteristic
	rxBuf     chan []byte   // inbound frames from TNC
	partial   []byte        // leftover bytes from a previous Read
	done      chan struct{} // closed by Close
	physical  chan struct{} // closed on physical BLE disconnect; unblocks Read
	closeOnce sync.Once
}

const bleMTU = 244 // maximum BLE ATT payload after MTU negotiation

func (c *bleMobilinkdConn) Read(p []byte) (int, error) {
	if len(c.partial) > 0 {
		n := copy(p, c.partial)
		c.partial = c.partial[n:]
		return n, nil
	}
	select {
	case buf, ok := <-c.rxBuf:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, buf)
		if n < len(buf) {
			c.partial = buf[n:]
		}
		return n, nil
	case <-c.done:
		return 0, io.EOF
	case <-c.physical:
		return 0, io.EOF
	}
}

func (c *bleMobilinkdConn) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > bleMTU {
			chunk = chunk[:bleMTU]
		}
		n, err := c.rxChar.WriteWithoutResponse(chunk)
		total += n
		if err != nil {
			return total, fmt.Errorf("BLE: write: %w", err)
		}
		p = p[len(chunk):]
	}
	return total, nil
}

func (c *bleMobilinkdConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		// Use a no-op rather than nil: DidDisconnectPeripheral fires the
		// connectHandler unconditionally, and calling a nil func panics.
		bluetooth.DefaultAdapter.SetConnectHandler(func(bluetooth.Device, bool) {})
		close(c.done)
		err = c.device.Disconnect()
	})
	return err
}
