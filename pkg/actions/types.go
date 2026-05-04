package actions

import "time"

// Status is the outcome of a single invocation attempt. Always one of
// the values in StatusValues; the runner panics on any other value.
type Status string

const (
	StatusOK           Status = "ok"
	StatusBadOTP       Status = "bad_otp"
	StatusBadArg       Status = "bad_arg"
	StatusDenied       Status = "denied"
	StatusDisabled     Status = "disabled"
	StatusUnknown      Status = "unknown"
	StatusNoCredential Status = "no_credential"
	StatusBusy         Status = "busy"
	StatusRateLimited  Status = "rate_limited"
	StatusTimeout      Status = "timeout"
	StatusError        Status = "error"
)

// Source is the inbound transport the invocation arrived on.
type Source string

const (
	SourceRF Source = "rf"
	SourceIS Source = "is"
)

// ArgMode controls how argv after the action verb is interpreted.
//
//	kv:       "@@<otp>#name k1=v1 k2=v2"  (current behavior, default)
//	freeform: "@@<otp>#name <one untokenized payload>"
type ArgMode string

const (
	ArgModeKV       ArgMode = "kv"
	ArgModeFreeform ArgMode = "freeform"
)

// FreeformArgKey is the synthetic key used for the single freeform
// value. Stable so executors, audit log, and webhook templates can
// refer to it as `arg` (env var GW_ARG, token {{arg}}). Not operator-
// settable.
const FreeformArgKey = "arg"

// ParsedInvocation is the output of parser.Parse. Args preserve key
// order as parsed off the wire so executors can present a stable argv.
//
// Args contains raw, untrusted key=value tokens straight off the wire.
// Callers MUST run them through the runner's sanitizer (Phase C) before
// passing them to an Executor.
//
// RawArgTail is the raw bytes after the action name and the first
// space, with no trimming or tokenization. Freeform consumers read it
// directly; kv consumers ignore it. May be populated even when Args is
// nil (kv tokenization failed but the action name parsed cleanly), so
// the classifier can still dispatch a freeform Action.
type ParsedInvocation struct {
	OTPDigits  string // empty if message had no OTP digits
	Action     string
	Args       []KeyValue
	RawArgTail string
}

type KeyValue struct {
	Key   string
	Value string
}

// Invocation is the runtime envelope passed to the Executor. The
// runner constructs it from a ParsedInvocation plus the matched
// configstore.Action and runtime context.
type Invocation struct {
	ID              uint64
	ActionID        uint
	ActionName      string
	SenderCall      string
	Source          Source
	OTPCredentialID uint // 0 if no credential was consulted
	OTPVerified     bool
	OTPCredName     string
	Args            []KeyValue
	StartedAt       time.Time
}

// Result is the executor outcome consumed by the runner.
type Result struct {
	Status        Status
	StatusDetail  string
	OutputCapture string
	ExitCode      *int
	HTTPStatus    *int
}
