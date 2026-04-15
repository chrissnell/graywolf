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

// modemBinaryName is the platform-specific filename for the modem binary.
const modemBinaryName = "graywolf-modem"
