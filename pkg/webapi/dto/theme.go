package dto

import (
	"errors"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// ThemeConfigRequest is the body accepted by PUT /api/preferences/theme.
// ID must match the kebab-case/lowercase pattern validated by
// configstore.IsValidTheme. The server does not enforce that the id
// matches a shipped theme — that's the frontend's responsibility, and
// keeping it so lets contributors add themes without touching Go.
type ThemeConfigRequest struct {
	ID string `json:"id"`
}

func (r ThemeConfigRequest) Validate() error {
	if !configstore.IsValidTheme(r.ID) {
		return errors.New("invalid theme id")
	}
	return nil
}

// ThemeConfigResponse is the body returned by GET and PUT on
// /api/preferences/theme.
type ThemeConfigResponse struct {
	ID string `json:"id"`
}
