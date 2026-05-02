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

// ParsedInvocation is the output of parser.Parse. Args preserve key
// order as parsed off the wire so executors can present a stable argv.
type ParsedInvocation struct {
	OTPDigits string // empty if message had no OTP digits
	Action    string
	Args      []KeyValue
}

type KeyValue struct {
	Key   string
	Value string
}

// Invocation is the runtime envelope passed to the Executor. The
// runner constructs it from a ParsedInvocation plus the matched
// configstore.Action and runtime context.
type Invocation struct {
	ID          uint64
	ActionID    uint
	ActionName  string
	SenderCall  string
	Source      Source
	OTPVerified bool
	OTPCredName string
	Args        []KeyValue
	StartedAt   time.Time
}

// Result is the executor outcome consumed by the runner.
type Result struct {
	Status        Status
	StatusDetail  string
	OutputCapture string
	ExitCode      *int
	HTTPStatus    *int
}
