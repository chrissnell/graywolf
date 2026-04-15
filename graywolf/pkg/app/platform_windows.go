//go:build windows

package app

import (
	"os"
	"path/filepath"
)

// defaultDBPath returns the default SQLite database path for Windows.
// Writing to Program Files requires elevation, so we use ProgramData
// which is writable by all local users.
func defaultDBPath() string {
	if pd := os.Getenv("ProgramData"); pd != "" {
		return filepath.Join(pd, "Graywolf", "graywolf.db")
	}
	return `C:\ProgramData\Graywolf\graywolf.db`
}

// defaultHistoryDBPath returns the default history database path for Windows.
func defaultHistoryDBPath() string {
	if pd := os.Getenv("ProgramData"); pd != "" {
		return filepath.Join(pd, "Graywolf", "graywolf-history.db")
	}
	return `C:\ProgramData\Graywolf\graywolf-history.db`
}

// modemBinaryName is the platform-specific filename for the modem binary.
const modemBinaryName = "graywolf-modem.exe"
