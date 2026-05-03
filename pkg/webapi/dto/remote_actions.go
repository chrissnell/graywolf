// Package dto wire shapes for the outbound Actions feature.
package dto

// RemoteOTPCredential is the safe wire shape for a remote-station TOTP
// secret. SecretB32 is intentionally absent — it is only echoed back
// at create time via RemoteOTPCredentialCreated, and never retrievable
// thereafter.
//
// UsedBy is the list of distinct uppercased target callsigns whose
// macros reference this credential. The credential cannot be deleted
// while non-empty (HTTP 409); the UI surfaces "Unbind from N macro(s)
// first" using its length.
type RemoteOTPCredential struct {
	ID         uint     `json:"id"`
	Name       string   `json:"name"`
	Algorithm  string   `json:"algorithm"`
	Digits     int      `json:"digits"`
	Period     int      `json:"period"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt *string  `json:"last_used_at,omitempty"`
	UsedBy     []string `json:"used_by"`
}

// RemoteOTPCredentialRequest is the create / update body. Algorithm,
// Digits, Period are optional on create; the server fills sha1/6/30.
type RemoteOTPCredentialRequest struct {
	Name      string `json:"name"`
	SecretB32 string `json:"secret_b32"`
	Algorithm string `json:"algorithm,omitempty"`
	Digits    int    `json:"digits,omitempty"`
	Period    int    `json:"period,omitempty"`
}

// RemoteActionMacro is the wire shape of one saved macro.
type RemoteActionMacro struct {
	ID                    uint   `json:"id"`
	TargetCall            string `json:"target_call"`
	Label                 string `json:"label"`
	ActionName            string `json:"action_name"`
	ArgsString            string `json:"args_string"`
	RemoteOTPCredentialID *uint  `json:"remote_otp_credential_id,omitempty"`
	Position              int    `json:"position"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`
}

// RemoteActionMacroRequest is the create / update body. TargetCall is
// uppercased on the server; clients may send any case.
//
// Update (PUT) semantics:
//
//   - Label, ActionName: gated — empty string leaves the field unchanged.
//   - ArgsString, RemoteOTPCredentialID: always overwrite. Empty string
//     clears args; nil unbinds the credential. Clients must send the
//     full update body.
//   - Position: ignored on PUT. The /macros/reorder endpoint is the
//     sole owner of macro ordering.
type RemoteActionMacroRequest struct {
	TargetCall            string `json:"target_call"`
	Label                 string `json:"label"`
	ActionName            string `json:"action_name"`
	ArgsString            string `json:"args_string"`
	RemoteOTPCredentialID *uint  `json:"remote_otp_credential_id,omitempty"`
	Position              int    `json:"position"`
}

// RemoteActionMacroReorderRequest is the body of POST
// /api/remote-actions/macros/reorder. The IDs list defines the new
// order: index 0 -> position 0, etc. Macros for the target that are
// not in the list are left unchanged.
type RemoteActionMacroReorderRequest struct {
	TargetCall string `json:"target_call"`
	IDs        []uint `json:"ids"`
}

// RemoteOTPCode is the response from POST /api/remote-actions/otp/{credId}.
// ExpiresAt is RFC3339 UTC and marks the inclusive upper edge of the
// step that produced this code. The drawer uses (ExpiresAt - now) as
// the countdown source.
type RemoteOTPCode struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
}
