package submit

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PendingDir is where 5xx-saved flare payloads land so the operator
// can retry later. $XDG_STATE_HOME wins when set; otherwise
// ~/.local/state/graywolf.
func PendingDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "graywolf")
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "state", "graywolf")
}

// SavePendingFlareAt writes the request body to
// <dir>/pending-flare-<unix-ts>.json so the operator can retry.
func SavePendingFlareAt(dir string, body []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	name := fmt.Sprintf("pending-flare-%d.json", time.Now().Unix())
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}
