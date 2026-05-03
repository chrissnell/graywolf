package webapi

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/remoteactions"
)

// newTestServerWithRemoteActions builds a *Server wired with a fresh
// in-memory SQLite that has only the remote-actions tables. The
// existing test helpers in this package construct the full configstore
// and are too heavy for the focused remote-actions tests.
func newTestServerWithRemoteActions(t *testing.T) (*Server, func()) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("fk on: %v", err)
	}
	for _, s := range []string{
		`CREATE TABLE remote_otp_credentials (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			secret_b32 TEXT NOT NULL,
			algorithm TEXT NOT NULL DEFAULT 'sha1',
			digits INTEGER NOT NULL DEFAULT 6,
			period INTEGER NOT NULL DEFAULT 30,
			created_at DATETIME NOT NULL,
			last_used_at DATETIME
		)`,
		`CREATE TABLE remote_action_macros (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_call TEXT NOT NULL,
			label TEXT NOT NULL,
			action_name TEXT NOT NULL,
			args_string TEXT NOT NULL DEFAULT '',
			remote_otp_credential_id INTEGER,
			position INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY (remote_otp_credential_id)
				REFERENCES remote_otp_credentials(id) ON DELETE SET NULL
		)`,
	} {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	svc, err := remoteactions.NewService(remoteactions.ServiceConfig{DB: db})
	if err != nil {
		t.Fatalf("svc: %v", err)
	}
	srv := &Server{}
	srv.SetRemoteActions(svc)
	return srv, func() {
		sqldb, _ := db.DB()
		if sqldb != nil {
			_ = sqldb.Close()
		}
	}
}
