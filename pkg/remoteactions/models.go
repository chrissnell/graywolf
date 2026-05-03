package remoteactions

import "time"

// RemoteOTPCredential is one named TOTP secret used to fire macros at a
// remote station. Stored plaintext per the single-user-station design;
// the REST list endpoints scrub SecretB32 from the wire shape.
//
// Name follows the convention "<CALLSIGN> OTP" (e.g. "NW5W OTP") but
// the column is just UNIQUE TEXT — operators can use whatever string
// they want.
//
// Algorithm/Digits/Period default at the SQL layer (sha1 / 6 / 30) so
// a row inserted with zero values still works.
type RemoteOTPCredential struct {
	ID         uint   `gorm:"primaryKey"`
	Name       string `gorm:"uniqueIndex;size:64;not null"`
	SecretB32  string `gorm:"size:128;not null"`
	Algorithm  string `gorm:"size:16;not null;default:'sha1'"`
	Digits     int    `gorm:"not null;default:6"`
	Period     int    `gorm:"not null;default:30"`
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

func (*RemoteOTPCredential) TableName() string { return "remote_otp_credentials" }

// RemoteActionMacro is one saved (target, action, args, optional
// credential) tuple shown as a tile in the Messages drawer.
//
// TargetCall is uppercased on write (validate.go); the column is plain
// TEXT with an index so per-thread lookup is a single seek.
//
// RemoteOTPCredentialID is nullable: when nil the macro fires in
// manual-OTP mode (operator types six digits before SEND). When set,
// the FK has ON DELETE SET NULL — deleting a credential demotes its
// macros instead of cascading.
//
// Position is the drag-reorder index, low first. Conflicts (two
// macros for the same target with the same position) are resolved by
// the store's reorder helper, not at the SQL layer.
type RemoteActionMacro struct {
	ID                    uint   `gorm:"primaryKey"`
	TargetCall            string `gorm:"size:9;not null;index:idx_remote_action_macros_target_call"`
	Label                 string `gorm:"size:64;not null"`
	ActionName            string `gorm:"size:32;not null"`
	ArgsString            string `gorm:"type:text;not null;default:''"`
	RemoteOTPCredentialID *uint  `gorm:"column:remote_otp_credential_id"`
	Position              int    `gorm:"not null;default:0"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (*RemoteActionMacro) TableName() string { return "remote_action_macros" }
