package configstore

import "gorm.io/gorm"

// migrateActionsTables creates the Actions feature tables. Uses raw SQL
// (not AutoMigrate) so the FK ON DELETE SET NULL clauses
// (actions.otp_credential_id, action_invocations.action_id,
// action_invocations.otp_credential_id) and the audit-log indexes can
// be expressed precisely. The four models (Action, OTPCredential,
// ActionListenerAddressee, ActionInvocation) are deliberately *not*
// added to the AutoMigrate list — this migration is the single source of
// truth for their schema.
//
// Audit-row FK rationale: ActionID and OTPCredentialID are nullable on
// ActionInvocation (the model declares *uint) so an unknown-action
// row or post-deletion lookup still writes; ON DELETE SET NULL keeps
// the audit row alive after operator deletes by nulling the dangling
// reference rather than orphaning it. ActionNameAt / OTPCredName are
// denormalized on the audit row for display continuity.
func migrateActionsTables(tx *gorm.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS otp_credentials (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			issuer TEXT NOT NULL DEFAULT '',
			account TEXT NOT NULL DEFAULT '',
			algorithm TEXT NOT NULL DEFAULT 'SHA1',
			digits INTEGER NOT NULL DEFAULT 6,
			period INTEGER NOT NULL DEFAULT 30,
			secret_b32 TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			last_used_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS actions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL,
			command_path TEXT NOT NULL DEFAULT '',
			working_dir TEXT NOT NULL DEFAULT '',
			webhook_method TEXT NOT NULL DEFAULT '',
			webhook_url TEXT NOT NULL DEFAULT '',
			webhook_headers TEXT NOT NULL DEFAULT '{}',
			webhook_body_template TEXT NOT NULL DEFAULT '',
			timeout_sec INTEGER NOT NULL DEFAULT 10,
			otp_required INTEGER NOT NULL DEFAULT 1,
			otp_credential_id INTEGER,
			sender_allowlist TEXT NOT NULL DEFAULT '',
			arg_schema TEXT NOT NULL DEFAULT '[]',
			rate_limit_sec INTEGER NOT NULL DEFAULT 5,
			queue_depth INTEGER NOT NULL DEFAULT 8,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY (otp_credential_id) REFERENCES otp_credentials(id) ON DELETE SET NULL
		)`,
		`CREATE TABLE IF NOT EXISTS action_listener_addressees (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			addressee TEXT NOT NULL UNIQUE,
			created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS action_invocations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			action_id INTEGER,
			action_name_at TEXT NOT NULL DEFAULT '',
			sender_call TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			otp_credential_id INTEGER,
			otp_verified INTEGER NOT NULL DEFAULT 0,
			raw_args_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL DEFAULT '',
			status_detail TEXT NOT NULL DEFAULT '',
			exit_code INTEGER,
			http_status INTEGER,
			output_capture TEXT NOT NULL DEFAULT '',
			reply_text TEXT NOT NULL DEFAULT '',
			truncated INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (action_id) REFERENCES actions(id) ON DELETE SET NULL,
			FOREIGN KEY (otp_credential_id) REFERENCES otp_credentials(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_action_invocations_action_id ON action_invocations(action_id)`,
		`CREATE INDEX IF NOT EXISTS idx_action_invocations_sender_call ON action_invocations(sender_call)`,
		`CREATE INDEX IF NOT EXISTS idx_action_invocations_created_at ON action_invocations(created_at)`,
	}
	for _, s := range stmts {
		if err := tx.Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}
