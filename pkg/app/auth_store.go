package app

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webauth"
)

// OpenAuthStore opens the configstore at dbPath and returns a ready
// webauth.AuthStore along with a cleanup function the caller must
// defer. It is the lightweight entry point used by the auth CLI
// subcommand, which needs database access without the full App
// lifecycle (no bridge, no governor, no HTTP server).
//
// The returned cleanup closes the underlying configstore; calling it
// twice is safe.
func OpenAuthStore(dbPath string) (*webauth.AuthStore, func(), error) {
	store, err := configstore.Open(dbPath)
	if err != nil {
		return nil, func() {}, fmt.Errorf("open configstore: %w", err)
	}
	authStore, err := webauth.NewAuthStore(store.DB())
	if err != nil {
		_ = store.Close()
		return nil, func() {}, fmt.Errorf("init auth store: %w", err)
	}
	closed := false
	cleanup := func() {
		if closed {
			return
		}
		closed = true
		_ = store.Close()
	}
	return authStore, cleanup, nil
}

// OpenStoreAndAuth is like OpenAuthStore but also exposes the
// underlying *configstore.Store so callers (currently only the auth
// CLI's set-password path) can write raw updates via store.DB().Save.
func OpenStoreAndAuth(dbPath string) (*configstore.Store, *webauth.AuthStore, func(), error) {
	store, err := configstore.Open(dbPath)
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("open configstore: %w", err)
	}
	authStore, err := webauth.NewAuthStore(store.DB())
	if err != nil {
		_ = store.Close()
		return nil, nil, func() {}, fmt.Errorf("init auth store: %w", err)
	}
	closed := false
	cleanup := func() {
		if closed {
			return
		}
		closed = true
		_ = store.Close()
	}
	return store, authStore, cleanup, nil
}
