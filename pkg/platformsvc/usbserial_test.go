//go:build android

package platformsvc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

func TestAvailableUsbSerialDevices_returnsDevices(t *testing.T) {
	respond := func(req *pb.PlatformMessage) *pb.PlatformMessage {
		if req.GetAvailableUsbSerialDevicesRequest() == nil {
			return nil
		}
		return &pb.PlatformMessage{Body: &pb.PlatformMessage_AvailableUsbSerialDevicesResponse{
			AvailableUsbSerialDevicesResponse: &pb.AvailableUsbSerialDevicesResponse{
				Devices: []*pb.AvailableUsbSerialDevicesResponse_Device{
					{VidPid: "2341:0043", Product: "TH-D75", Manufacturer: "Kenwood", HasPermission: true},
					{VidPid: "10c4:ea60", Product: "Digirig", Manufacturer: "Silicon Labs", HasPermission: false},
				},
			},
		}}
	}
	withFakeClient(t, respond, func(c *clientImpl) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		got, err := c.AvailableUsbSerialDevices(ctx)
		if err != nil {
			t.Fatalf("AvailableUsbSerialDevices: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 devices, got %d", len(got))
		}
		if got[0].VidPid != "2341:0043" || got[0].Product != "TH-D75" || !got[0].HasPermission {
			t.Errorf("device[0] = %+v", got[0])
		}
		if got[1].VidPid != "10c4:ea60" || got[1].HasPermission {
			t.Errorf("device[1] = %+v", got[1])
		}
	})
}

func TestUsbSerialOpen_roundTrip(t *testing.T) {
	srv, c := newBtTestServer(t)
	defer srv.close()

	openDone := make(chan error, 1)
	rwcCh := make(chan io.ReadWriteCloser, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rwc, err := c.UsbSerialOpen(ctx, "2341:0043", 9600)
		if err != nil {
			openDone <- err
			return
		}
		rwcCh <- rwc
		openDone <- nil
	}()

	open := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialOpen() != nil
	}, 2*time.Second)
	if got, want := open.GetSerialOpen().GetAddress(), "2341:0043"; got != want {
		t.Fatalf("SerialOpen address: got %q want %q", got, want)
	}
	if open.GetSerialOpen().GetKind() != pb.SerialKind_SERIAL_KIND_USB {
		t.Fatalf("SerialOpen kind: got %v want USB", open.GetSerialOpen().GetKind())
	}
	if got := open.GetSerialOpen().GetBaud(); got != 9600 {
		t.Fatalf("SerialOpen baud: got %d want 9600", got)
	}
	handle := open.GetSerialOpen().GetHandle()
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpenAck{
		SerialOpenAck: &pb.SerialOpenAck{Handle: handle, Ok: true},
	}})

	if err := <-openDone; err != nil {
		t.Fatalf("UsbSerialOpen: %v", err)
	}
	rwc := <-rwcCh
	defer rwc.Close()

	writeDone := make(chan error, 1)
	go func() {
		_, err := rwc.Write([]byte("KISS"))
		writeDone <- err
	}()
	data := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialData() != nil
	}, 2*time.Second)
	if !bytes.Equal(data.GetSerialData().GetData(), []byte("KISS")) {
		t.Fatalf("client wrote %q", data.GetSerialData().GetData())
	}
	if got := data.GetSerialData().GetHandle(); got != handle {
		t.Fatalf("handle mismatch: got %d want %d", got, handle)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("Write: %v", err)
	}

	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialData{
		SerialData: &pb.SerialData{Handle: handle, Data: []byte("ACK!")},
	}})
	buf := make([]byte, 16)
	n, err := rwc.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(buf[:n], []byte("ACK!")) {
		t.Fatalf("Read got %q", buf[:n])
	}
}

func TestUsbSerialOpen_serverClose_returnsEOF(t *testing.T) {
	srv, c := newBtTestServer(t)
	defer srv.close()

	openDone := make(chan io.ReadWriteCloser, 1)
	openErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rwc, err := c.UsbSerialOpen(ctx, "2341:0043", 9600)
		if err != nil {
			openErr <- err
			return
		}
		openDone <- rwc
	}()

	open := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialOpen() != nil
	}, 2*time.Second)
	handle := open.GetSerialOpen().GetHandle()
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpenAck{
		SerialOpenAck: &pb.SerialOpenAck{Handle: handle, Ok: true},
	}})

	var rwc io.ReadWriteCloser
	select {
	case rwc = <-openDone:
	case err := <-openErr:
		t.Fatalf("UsbSerialOpen: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("UsbSerialOpen did not return")
	}
	defer rwc.Close()

	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialClose{
		SerialClose: &pb.SerialClose{Handle: handle, Reason: "usb_detached"},
	}})

	buf := make([]byte, 16)
	if _, err := rwc.Read(buf); err != io.EOF {
		t.Fatalf("Read err: got %v want io.EOF", err)
	}
}

func TestUsbSerialOpen_serialError_returnsTypedError(t *testing.T) {
	srv, c := newBtTestServer(t)
	defer srv.close()

	openDone := make(chan io.ReadWriteCloser, 1)
	openErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rwc, err := c.UsbSerialOpen(ctx, "2341:0043", 9600)
		if err != nil {
			openErr <- err
			return
		}
		openDone <- rwc
	}()

	open := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialOpen() != nil
	}, 2*time.Second)
	handle := open.GetSerialOpen().GetHandle()
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpenAck{
		SerialOpenAck: &pb.SerialOpenAck{Handle: handle, Ok: true},
	}})

	var rwc io.ReadWriteCloser
	select {
	case rwc = <-openDone:
	case err := <-openErr:
		t.Fatalf("UsbSerialOpen: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("UsbSerialOpen did not return")
	}
	defer rwc.Close()

	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialError{
		SerialError: &pb.SerialError{Handle: handle, Code: "usb_detached", Detail: "device unplugged"},
	}})

	buf := make([]byte, 16)
	_, err := rwc.Read(buf)
	var serr *SerialErrorErr
	if !errors.As(err, &serr) {
		t.Fatalf("Read err: got %T %v, want *SerialErrorErr", err, err)
	}
	if serr.Code != "usb_detached" {
		t.Errorf("SerialErrorErr.Code = %q want %q", serr.Code, "usb_detached")
	}
}

func TestUsbSerialOpen_ackDenied_returnsError(t *testing.T) {
	srv, c := newBtTestServer(t)
	defer srv.close()

	openErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := c.UsbSerialOpen(ctx, "2341:0043", 9600)
		openErr <- err
	}()

	open := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialOpen() != nil
	}, 2*time.Second)
	handle := open.GetSerialOpen().GetHandle()
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpenAck{
		SerialOpenAck: &pb.SerialOpenAck{Handle: handle, Ok: false, Error: "permission_denied"},
	}})

	select {
	case err := <-openErr:
		if err == nil {
			t.Fatal("expected error on denied ack")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("UsbSerialOpen did not return on denied ack")
	}
}
