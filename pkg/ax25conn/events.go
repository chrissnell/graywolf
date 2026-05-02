package ax25conn

import "time"

// EventKind categorizes the inputs to the session goroutine.
type EventKind uint8

const (
	EventFrameRX EventKind = iota + 1
	EventDataTX             // operator typed bytes
	EventConnect            // operator pressed Connect
	EventDisconnect         // operator pressed Disconnect
	EventAbort              // operator pressed Abort
	EventT1Expiry
	EventT2Expiry
	EventT3Expiry
	EventHeartbeat // 5s housekeeping tick
	EventStatsTick // 1Hz CONNECTED telemetry emission
	EventShutdown  // manager tearing us down
)

// Event is one input dispatched to the per-session run loop.
type Event struct {
	Kind  EventKind
	Frame *Frame // EventFrameRX only
	Data  []byte // EventDataTX only
}

// OutEventKind enumerates events the session emits to its observer
// (the WebSocket bridge in Phase 2 / a test sink in Phase 1).
type OutEventKind uint8

const (
	OutStateChange OutEventKind = iota + 1
	OutDataRX
	OutLinkStats
	OutError
)

// OutEvent is the envelope the session emits via its Observer hook.
type OutEvent struct {
	Kind    OutEventKind
	State   State     // OutStateChange
	Data    []byte    // OutDataRX
	Stats   LinkStats // OutLinkStats
	ErrCode string    // OutError
	ErrMsg  string    // OutError
}

// LinkStats is the snapshot the bridge can render in the telemetry
// side panel.
type LinkStats struct {
	State      State
	VS, VR, VA uint8
	RC         int
	PeerBusy   bool
	OwnBusy    bool
	FramesTX   uint64
	FramesRX   uint64
	BytesTX    uint64
	BytesRX    uint64
	RTT        time.Duration
}
