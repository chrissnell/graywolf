package webauth

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWebUserLastSeenColumn confirms AutoMigrate picks up the new
// release-notes-ack column and that existing rows (pre-feature) read
// back as the empty string.
func TestWebUserLastSeenColumn(t *testing.T) {
	s := testAuthStore(t)

	// Sanity: column must exist on the schema.
	if !s.db.Migrator().HasColumn(&WebUser{}, "last_seen_release_version") {
		t.Fatal("expected last_seen_release_version column on web_users")
	}

	// Insert a user with the zero value (simulates an existing row
	// migrated in from before the feature landed).
	u := &WebUser{Username: "old", PasswordHash: "x"}
	if err := s.db.Create(u).Error; err != nil {
		t.Fatalf("create legacy user: %v", err)
	}
	got, err := s.GetLastSeenReleaseVersion(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("GetLastSeenReleaseVersion: %v", err)
	}
	if got != "" {
		t.Fatalf("legacy user should have empty last_seen, got %q", got)
	}
}

// TestSetAndGetLastSeenReleaseVersion round-trips the ack field.
func TestSetAndGetLastSeenReleaseVersion(t *testing.T) {
	s := testAuthStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "ops", "hash", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetLastSeenReleaseVersion(ctx, u.ID, "0.11.0"); err != nil {
		t.Fatalf("SetLastSeenReleaseVersion: %v", err)
	}
	got, err := s.GetLastSeenReleaseVersion(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetLastSeenReleaseVersion: %v", err)
	}
	if got != "0.11.0" {
		t.Fatalf("want 0.11.0, got %q", got)
	}

	// Idempotent: re-setting to the same value succeeds.
	if err := s.SetLastSeenReleaseVersion(ctx, u.ID, "0.11.0"); err != nil {
		t.Fatal(err)
	}
	// Overwriting to a newer version sticks.
	if err := s.SetLastSeenReleaseVersion(ctx, u.ID, "0.12.0"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetLastSeenReleaseVersion(ctx, u.ID)
	if got != "0.12.0" {
		t.Fatalf("want 0.12.0 after overwrite, got %q", got)
	}

	// A stale session whose user was deleted between auth and ack
	// writes zero rows and must surface an error — the ack handler
	// returns 204 on nil, so silent no-op would lie to the client.
	if err := s.SetLastSeenReleaseVersion(ctx, 999999, "0.13.0"); err == nil {
		t.Fatal("SetLastSeenReleaseVersion for missing user: want error, got nil")
	}
}

// TestNewUsersAreSeededWithBuildVersion covers the anti-backlog
// invariant: CreateUser and CreateFirstUser both carry the supplied
// buildVersion into LastSeenReleaseVersion.
func TestNewUsersAreSeededWithBuildVersion(t *testing.T) {
	s := testAuthStore(t)
	ctx := context.Background()

	u, err := s.CreateFirstUser(ctx, "first", "h", "0.11.0")
	if err != nil {
		t.Fatal(err)
	}
	if u.LastSeenReleaseVersion != "0.11.0" {
		t.Fatalf("first user seed: got %q", u.LastSeenReleaseVersion)
	}
	got, _ := s.GetLastSeenReleaseVersion(ctx, u.ID)
	if got != "0.11.0" {
		t.Fatalf("persisted: got %q", got)
	}

	u2, err := s.CreateUser(ctx, "second", "h", "0.12.0")
	if err != nil {
		t.Fatal(err)
	}
	if u2.LastSeenReleaseVersion != "0.12.0" {
		t.Fatalf("second user seed: got %q", u2.LastSeenReleaseVersion)
	}
}

// TestSetupHandlerSeedsFromHandlerVersion drives the whole
// CreateFirstUser HTTP flow and confirms the Handlers.BuildVersion is
// what lands in the DB.
func TestSetupHandlerSeedsFromHandlerVersion(t *testing.T) {
	s := testAuthStore(t)
	h := &Handlers{Auth: s, BuildVersion: "0.11.0"}

	req := httptest.NewRequest("POST", "/api/auth/setup",
		strings.NewReader(`{"username":"admin","password":"pw"}`))
	rec := httptest.NewRecorder()
	h.CreateFirstUser(rec, req)
	if rec.Code != 201 {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}

	u, err := s.GetUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.LastSeenReleaseVersion != "0.11.0" {
		t.Fatalf("admin seeded with %q; expected 0.11.0", u.LastSeenReleaseVersion)
	}
}

// TestLastSeenColumnSizeBound sanity-checks the size:20 gorm tag by
// storing the longest plausible version string. We don't assert the
// column type literally (SQLite would silently accept anything) but
// we do assert that a 20-char semver round-trips correctly.
func TestLastSeenColumnSizeBound(t *testing.T) {
	s := testAuthStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "u", "h", "")
	if err != nil {
		t.Fatal(err)
	}
	// 20-char version: e.g. "999.999.999.999.999" is 19 chars; pad
	// with one leading zero via padding. Simpler: a 20-char string.
	v := "12345.67890.12345.67" // 20 chars (ok: 20 fits)
	if len(v) != 20 {
		t.Fatalf("test bug: len=%d", len(v))
	}
	if err := s.SetLastSeenReleaseVersion(ctx, u.ID, v); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetLastSeenReleaseVersion(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != v {
		t.Fatalf("round-trip mismatch: got %q want %q", got, v)
	}
}
