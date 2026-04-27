package modembridge

import (
	"errors"
	"io"
	"log/slog"
	"sync"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// ipcLoop owns one modem IPC conversation: a single length-prefixed
// protobuf stream with a write mutex that serialises transmits, plus a
// read loop that dispatches every inbound IpcMessage to an onMessage
// callback.
//
// ipcLoop is not responsible for handshakes, configuration push, or
// graceful shutdown orchestration; those stay in session.go where the
// protocol state lives. ipcLoop is purely the framing-level Send/Run
// primitive.
type ipcLoop struct {
	conn    sessionConn
	logger  *slog.Logger
	writeMu sync.Mutex
}

func newIpcLoop(conn sessionConn, logger *slog.Logger) *ipcLoop {
	if logger == nil {
		logger = slog.Default()
	}
	return &ipcLoop{conn: conn, logger: logger}
}

// Send writes msg as a length-prefixed frame. The write mutex ensures
// concurrent senders (e.g. the txgovernor pushing TransmitFrame while
// the session goroutine pushes ConfigureChannel) do not interleave.
func (l *ipcLoop) Send(msg *pb.IpcMessage) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return writeFrame(l.conn, msg)
}

// Run reads framed messages off the connection and invokes onMessage
// for each one. It returns nil on a clean EOF (peer half-closed between
// frames) and the underlying error on any other failure. Callers stop
// the loop by closing the underlying conn, which surfaces here as a
// read error.
func (l *ipcLoop) Run(onMessage func(*pb.IpcMessage)) error {
	for {
		msg, err := readFrame(l.conn)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		onMessage(msg)
	}
}
