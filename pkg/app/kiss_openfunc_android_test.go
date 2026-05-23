//go:build android

package app

import (
	"context"
	"errors"
	"io"
	"testing"
)

// fakePsv records BtSerialOpen / UsbSerialOpen calls and returns a canned rwc/err.
type fakePsv struct {
	macSeen    string
	vidPidSeen string
	baudSeen   uint32
	rwc        io.ReadWriteCloser
	err        error
}

func (f *fakePsv) BtSerialOpen(_ context.Context, mac string) (io.ReadWriteCloser, error) {
	f.macSeen = mac
	return f.rwc, f.err
}

func (f *fakePsv) UsbSerialOpen(_ context.Context, vidPid string, baud uint32) (io.ReadWriteCloser, error) {
	f.vidPidSeen = vidPid
	f.baudSeen = baud
	return f.rwc, f.err
}

type nopRWC struct{}

func (nopRWC) Read(_ []byte) (int, error)  { return 0, io.EOF }
func (nopRWC) Write(_ []byte) (int, error) { return 0, nil }
func (nopRWC) Close() error                { return nil }

func TestNewKissSerialOpenFunc_Android_BluetoothMAC_RoutesThroughPsv(t *testing.T) {
	f := &fakePsv{rwc: nopRWC{}}
	open := newKissSerialOpenFunc(f)
	if open == nil {
		t.Fatal("expected non-nil OpenFunc")
	}
	rwc, err := open("AA:BB:CC:00:00:01", 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if rwc == nil {
		t.Fatalf("rwc=nil")
	}
	if f.macSeen != "AA:BB:CC:00:00:01" {
		t.Fatalf("mac=%q", f.macSeen)
	}
}

func TestNewKissSerialOpenFunc_Android_HyphenMAC_RoutesThroughPsv(t *testing.T) {
	f := &fakePsv{rwc: nopRWC{}}
	open := newKissSerialOpenFunc(f)
	if _, err := open("aa-bb-cc-00-00-01", 0); err != nil {
		t.Fatalf("open: %v", err)
	}
	if f.macSeen != "aa-bb-cc-00-00-01" {
		t.Fatalf("mac=%q", f.macSeen)
	}
}

func TestNewKissSerialOpenFunc_Android_NonMAC_RejectsWithoutCallingPsv(t *testing.T) {
	f := &fakePsv{}
	open := newKissSerialOpenFunc(f)
	_, err := open("/dev/ttyUSB0", 9600)
	if !errors.Is(err, errNotSupportedOnAndroid) {
		t.Fatalf("err=%v want errNotSupportedOnAndroid", err)
	}
	if f.macSeen != "" {
		t.Fatalf("BtSerialOpen called with %q", f.macSeen)
	}
}

func TestNewKissSerialOpenFunc_Android_NilClient_ReturnsNil(t *testing.T) {
	if got := newKissSerialOpenFunc(nil); got != nil {
		t.Fatalf("expected nil OpenFunc when psv=nil; got non-nil")
	}
}

func TestNewKissSerialOpenFunc_Android_VidPid_RoutesThroughUsb(t *testing.T) {
	f := &fakePsv{rwc: nopRWC{}}
	open := newKissSerialOpenFunc(f)
	if open == nil {
		t.Fatal("expected non-nil OpenFunc")
	}
	rwc, err := open("2341:0043", 9600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if rwc == nil {
		t.Fatalf("rwc=nil")
	}
	if f.vidPidSeen != "2341:0043" {
		t.Fatalf("vidPid=%q", f.vidPidSeen)
	}
	if f.baudSeen != 9600 {
		t.Fatalf("baud=%d want 9600", f.baudSeen)
	}
	if f.macSeen != "" {
		t.Fatalf("BtSerialOpen wrongly called with %q", f.macSeen)
	}
}
