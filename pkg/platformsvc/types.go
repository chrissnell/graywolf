//go:build android

package platformsvc

import (
	"errors"
	"fmt"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

const SchemaVersion uint32 = 1

type PttMethod = pb.PttMethod
type UsbClass = pb.UsbClass
type UsbDevice = pb.UsbDevice
type GpsFix = pb.GpsFix
type AudioRouteChanged = pb.AudioRouteChanged
type HelloResponse = pb.Hello
type PttAck = pb.PttAck

// UsbHandle is an opaque token returned by SelectUsbDevice and reused on
// subsequent PTT requests. Equivalent to UsbSelectResponse.handle_id but
// typed so callers can't accidentally pass a vid/pid string.
type UsbHandle struct {
	ID  string
	Vid uint16
	Pid uint16
}

// ErrSchemaMismatch is returned by Hello when the server's schema version
// does not match the client's. The client does NOT retry on this error.
type ErrSchemaMismatch struct {
	ClientVersion uint32
	ServerVersion uint32
}

func (e *ErrSchemaMismatch) Error() string {
	return fmt.Sprintf("platformsvc: schema mismatch (client=%d, server=%d)",
		e.ClientVersion, e.ServerVersion)
}

// ErrServerError is returned when the server replies with an Error message.
type ErrServerError struct {
	Code    pb.ErrorCode
	Message string
}

func (e *ErrServerError) Error() string {
	return fmt.Sprintf("platformsvc: server error %s: %s", e.Code, e.Message)
}

var ErrClosed = errors.New("platformsvc: client closed")

// ErrDisconnected is returned by an in-flight request whose underlying
// connection died (read EOF, write error). The reconnect loop will
// re-dial; callers can retry idempotent requests after that succeeds.
var ErrDisconnected = errors.New("platformsvc: connection disconnected")
