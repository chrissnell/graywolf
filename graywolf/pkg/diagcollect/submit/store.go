package submit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// StorageDir returns the directory the per-flare token files live in.
// $XDG_STATE_HOME wins when set; otherwise ~/.local/state/graywolf/flares.
func StorageDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "graywolf", "flares")
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "state", "graywolf", "flares")
}

// PendingDir is StorageDir's parent — saves on 5xx land here.
func PendingDir() string {
	return filepath.Dir(StorageDir())
}

// SaveTokenAt writes resp to <dir>/<flare-id>.json (mode 0600).
func SaveTokenAt(dir string, resp flareschema.SubmitResponse) (string, error) {
	if err := validateFlareID(resp.FlareID); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, resp.FlareID+".json")
	body, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// LoadTokenAt reads a previously-saved response by flare-id.
func LoadTokenAt(dir, flareID string) (flareschema.SubmitResponse, error) {
	if err := validateFlareID(flareID); err != nil {
		return flareschema.SubmitResponse{}, err
	}
	path := filepath.Join(dir, flareID+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		return flareschema.SubmitResponse{}, fmt.Errorf("read %s: %w", path, err)
	}
	var resp flareschema.SubmitResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return flareschema.SubmitResponse{}, fmt.Errorf("decode: %w", err)
	}
	return resp, nil
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

// validateFlareID rejects anything that could escape the storage
// directory or shadow a sibling file.
func validateFlareID(id string) error {
	if id == "" {
		return errors.New("empty flare-id")
	}
	if strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return fmt.Errorf("flare-id %q contains path separators or traversal", id)
	}
	return nil
}
