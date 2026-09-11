package cot

// SendErrorKind classifies a CoT send failure so the webapi handler can
// map it to an HTTP status without string-matching. Mirrors
// beacon.SendNowErrorKind.
type SendErrorKind int

const (
	// SendErrorCallsign means the station callsign could not be
	// resolved (empty or still N0CALL). CoT objects always transmit
	// under the inherited station callsign -- there is no per-target
	// override.
	SendErrorCallsign SendErrorKind = iota
	// SendErrorEncode is an AX.25 frame-encode failure (malformed
	// destination/path, or a callsign that resolved but doesn't parse).
	SendErrorEncode
	// SendErrorChannelMode means the target channel is in packet-only
	// mode and cannot transmit APRS CoT objects.
	SendErrorChannelMode
	// SendErrorSubmit means the TX governor refused the frame, the
	// APRS-IS sink is unavailable, or the send otherwise failed at the
	// transport layer.
	SendErrorSubmit
)

func (k SendErrorKind) String() string {
	switch k {
	case SendErrorCallsign:
		return "callsign"
	case SendErrorEncode:
		return "encode"
	case SendErrorChannelMode:
		return "channel_mode"
	case SendErrorSubmit:
		return "submit"
	default:
		return "unknown"
	}
}

// SendError is the typed error returned by Scheduler.SendNow and
// Scheduler.SendScheduled on failure. Kind selects the failure
// category; Err preserves the underlying cause for unwrap/errors.Is.
type SendError struct {
	Kind SendErrorKind
	Err  error
}

func (e *SendError) Error() string {
	if e.Err == nil {
		return e.Kind.String()
	}
	return e.Err.Error()
}

func (e *SendError) Unwrap() error { return e.Err }
