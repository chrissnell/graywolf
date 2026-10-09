//go:build darwin && cgo

package kiss

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Foundation -framework IOBluetooth
#include <stdlib.h>
#include "rfcomm_darwin.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	"github.com/chrissnell/graywolf/pkg/mainthread"
)

// classicBluetoothNative is true when this build opens Bluetooth Classic
// addresses itself (IOBluetooth) instead of via a tty.
const classicBluetoothNative = true

// ClassicBluetoothSupported reports whether OpenClassicBluetooth and
// PairedClassicBluetooth work in this build.
func ClassicBluetoothSupported() bool { return true }

const nativeErrLen = 256

func nativeErr(buf *[nativeErrLen]C.char) string {
	return C.GoString(&buf[0])
}

// OpenClassicBluetooth dials the Serial Port service of the paired device
// at addr and returns the byte stream. channel 0 looks the channel up via
// SDP. If the radio has already reconnected to this Mac by itself, that
// link is dropped first (the radio accepts one serial connection).
func OpenClassicBluetooth(addr string, channel int) (io.ReadWriteCloser, error) {
	if !mainthread.Running() {
		return nil, errors.New("bluetooth: the main-thread run loop is not running " +
			"(IOBluetooth callbacks need it; the program must start via mainthread.Run)")
	}
	caddr := C.CString(addr)
	defer C.free(unsafe.Pointer(caddr))
	var (
		rfd  C.int
		cerr [nativeErrLen]C.char
	)
	h := C.gw_rfcomm_open(caddr, C.int(channel), &rfd, &cerr[0], nativeErrLen)
	if h == 0 {
		return nil, fmt.Errorf("bluetooth %s: %s", addr, nativeErr(&cerr))
	}
	if err := syscall.SetNonblock(int(rfd), true); err != nil {
		C.gw_rfcomm_close(h)
		_ = syscall.Close(int(rfd))
		return nil, fmt.Errorf("bluetooth %s: %w", addr, err)
	}
	r := os.NewFile(uintptr(rfd), "bluetooth-rfcomm")
	return &btConn{
		r: r,
		write: func(p []byte) error {
			var e [nativeErrLen]C.char
			if C.gw_rfcomm_write(h, unsafe.Pointer(&p[0]), C.int(len(p)),
				C.int(classicBtWriteTimeout.Milliseconds()), &e[0], nativeErrLen) != 0 {
				return errors.New("bluetooth write: " + nativeErr(&e))
			}
			return nil
		},
		closeFn: func() { C.gw_rfcomm_close(h) },
	}, nil
}

// PairedClassicBluetooth lists the Bluetooth devices paired with this Mac.
func PairedClassicBluetooth() ([]ClassicBtDevice, error) {
	cs := C.gw_bt_paired_json()
	if cs == nil {
		return nil, errors.New("bluetooth: cannot list paired devices")
	}
	defer C.free(unsafe.Pointer(cs))
	var raw []struct {
		MAC  string `json:"mac"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(C.GoString(cs)), &raw); err != nil {
		return nil, err
	}
	out := make([]ClassicBtDevice, 0, len(raw))
	for _, d := range raw {
		out = append(out, ClassicBtDevice{MAC: d.MAC, Name: d.Name})
	}
	return out, nil
}
