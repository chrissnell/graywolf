//go:build !windows

package app

// defaultDBPath returns the default SQLite database path for Unix systems.
func defaultDBPath() string {
	return "./graywolf.db"
}

// defaultHistoryDBPath returns the default history database path for Unix systems.
func defaultHistoryDBPath() string {
	return "./graywolf-history.db"
}

// defaultTileCacheDir returns the default offline PMTiles cache directory
// for Unix systems. Placed next to the SQLite config DB so a single
// service-manager configured working directory holds all of graywolf's
// state.
func defaultTileCacheDir() string {
	return "./tiles"
}

// modemBinaryName is the platform-specific filename for the modem binary.
const modemBinaryName = "graywolf-modem"
