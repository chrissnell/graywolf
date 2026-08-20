//go:build android

package platformsvc

import (
	"context"
	"fmt"
	"io"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// ScanBLEKISS sends a BleScanRequest to the Kotlin platform service, then
// delivers BleScanResult pushes to discovered until ctx is cancelled.
// A BleScanStop is sent on return so the Android BLE scanner releases its
// wake lock promptly.
func (c *clientImpl) ScanBLEKISS(ctx context.Context, discovered func(BLEKISSDevice)) error {
	if c.closed.Load() {
		return ErrClosed
	}

	resCh := make(chan *pb.BleScanResult, 32)
	errCh := make(chan *pb.BleScanError, 1)
	c.subsMu.Lock()
	c.bleScanSubs = append(c.bleScanSubs, resCh)
	c.bleScanErrSubs = append(c.bleScanErrSubs, errCh)
	c.subsMu.Unlock()

	defer func() {
		// Unsubscribe both channels.
		c.subsMu.Lock()
		for i, s := range c.bleScanSubs {
			if s == resCh {
				c.bleScanSubs = append(c.bleScanSubs[:i], c.bleScanSubs[i+1:]...)
				break
			}
		}
		for i, s := range c.bleScanErrSubs {
			if s == errCh {
				c.bleScanErrSubs = append(c.bleScanErrSubs[:i], c.bleScanErrSubs[i+1:]...)
				break
			}
		}
		c.subsMu.Unlock()

		// Tell the server to stop the scan.
		_ = c.send(&pb.PlatformMessage{Body: &pb.PlatformMessage_BleScanStop{
			BleScanStop: &pb.BleScanStop{},
		}})
	}()

	if err := c.send(&pb.PlatformMessage{Body: &pb.PlatformMessage_BleScanRequest{
		BleScanRequest: &pb.BleScanRequest{},
	}}); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.closeCh:
			return ErrClosed
		case e, ok := <-errCh:
			if !ok {
				return ErrDisconnected
			}
			return fmt.Errorf("BLE scan error [%s]: %s", e.GetCode(), e.GetDetail())
		case r, ok := <-resCh:
			if !ok {
				return ErrDisconnected
			}
			discovered(BLEKISSDevice{
				Addr: r.GetAddr(),
				Name: r.GetName(),
				RSSI: int16(r.GetRssi()),
			})
		}
	}
}

// BLEOpen opens a BLE GATT KISS stream to addr using the shared serial-stream
// protocol with SERIAL_KIND_BLE. The returned ReadWriteCloser is multiplexed
// on the shared UDS connection; Close tears down the GATT link server-side.
func (c *clientImpl) BLEOpen(ctx context.Context, addr string) (io.ReadWriteCloser, error) {
	return c.openSerialStream(ctx, func(handle uint32) *pb.SerialOpen {
		return &pb.SerialOpen{
			Handle:  handle,
			Kind:    pb.SerialKind_SERIAL_KIND_BLE,
			Address: addr,
		}
	})
}
