// Package dto: actions resource shapes.
//
// The Actions REST API uses one wire struct per resource for both
// reads and writes — the operator-facing form binds against the same
// shape it gets back from GET. ID and derived fields
// (LastInvokedAt/LastInvokedBy) are populated only on reads and
// ignored on writes.
package dto

// Action is the wire shape for an Action definition.
type Action struct {
	ID                  uint              `json:"id"`
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Type                string            `json:"type"` // "command" | "webhook"
	CommandPath         string            `json:"command_path,omitempty"`
	WorkingDir          string            `json:"working_dir,omitempty"`
	WebhookMethod       string            `json:"webhook_method,omitempty"`
	WebhookURL          string            `json:"webhook_url,omitempty"`
	WebhookHeaders      map[string]string `json:"webhook_headers,omitempty"`
	WebhookBodyTemplate string            `json:"webhook_body_template,omitempty"`
	TimeoutSec          int               `json:"timeout_sec"`
	OTPRequired         bool              `json:"otp_required"`
	OTPCredentialID     *uint             `json:"otp_credential_id,omitempty"`
	SenderAllowlist     string            `json:"sender_allowlist"`
	ArgSchema           []ArgSpec         `json:"arg_schema"`
	RateLimitSec        int               `json:"rate_limit_sec"`
	QueueDepth          int               `json:"queue_depth"`
	Enabled             bool              `json:"enabled"`
	LastInvokedAt       *string           `json:"last_invoked_at,omitempty"`
	LastInvokedBy       string            `json:"last_invoked_by,omitempty"`
}

// ArgSpec is one entry in an Action's arg_schema.
type ArgSpec struct {
	Key      string `json:"key"`
	Regex    string `json:"regex,omitempty"`
	MaxLen   int    `json:"max_len,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// OTPCredential is the safe wire shape for a TOTP credential. SecretB32
// is intentionally absent — see OTPCredentialCreated for the one-shot
// reveal on POST.
type OTPCredential struct {
	ID         uint     `json:"id"`
	Name       string   `json:"name"`
	Issuer     string   `json:"issuer"`
	Account    string   `json:"account"`
	Algorithm  string   `json:"algorithm"`
	Digits     int      `json:"digits"`
	Period     int      `json:"period"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt *string  `json:"last_used_at,omitempty"`
	UsedBy     []string `json:"used_by,omitempty"` // Action names that reference this cred
}

// OTPCredentialCreated is the response body for POST
// /api/otp-credentials. SecretB32 and OtpAuthURI are returned only on
// this response and never read back.
type OTPCredentialCreated struct {
	OTPCredential
	SecretB32  string `json:"secret_b32"`
	OtpAuthURI string `json:"otpauth_uri"`
}

// ActionListenerAddressee is one extra APRS addressee that triggers
// the Actions classifier (independent of the station call and tactical
// aliases).
type ActionListenerAddressee struct {
	ID        uint   `json:"id"`
	Addressee string `json:"addressee"`
	CreatedAt string `json:"created_at"`
}

// ActionInvocation is one audit row.
type ActionInvocation struct {
	ID            uint              `json:"id"`
	ActionID      *uint             `json:"action_id,omitempty"`
	ActionName    string            `json:"action_name"`
	SenderCall    string            `json:"sender_call"`
	Source        string            `json:"source"`
	OTPCredID     *uint             `json:"otp_credential_id,omitempty"`
	OTPVerified   bool              `json:"otp_verified"`
	Args          map[string]string `json:"args"`
	Status        string            `json:"status"`
	StatusDetail  string            `json:"status_detail,omitempty"`
	ExitCode      *int              `json:"exit_code,omitempty"`
	HTTPStatus    *int              `json:"http_status,omitempty"`
	OutputCapture string            `json:"output_capture,omitempty"`
	ReplyText     string            `json:"reply_text"`
	Truncated     bool              `json:"truncated"`
	CreatedAt     string            `json:"created_at"`
}

// TestFireRequest is the body accepted by POST /api/actions/{id}/test-fire.
type TestFireRequest struct {
	Args map[string]string `json:"args"`
}

// TestFireResponse is the body returned by POST /api/actions/{id}/test-fire.
//
// Truncated mirrors the value the audit row would have stored for a
// real on-air invocation, so the UI can warn the operator that their
// reply got chopped to fit the 67-char APRS message cap.
type TestFireResponse struct {
	Status        string `json:"status"`
	StatusDetail  string `json:"status_detail,omitempty"`
	OutputCapture string `json:"output_capture,omitempty"`
	ReplyText     string `json:"reply_text"`
	Truncated     bool   `json:"truncated"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	HTTPStatus    *int   `json:"http_status,omitempty"`
	InvocationID  uint   `json:"invocation_id"`
}
