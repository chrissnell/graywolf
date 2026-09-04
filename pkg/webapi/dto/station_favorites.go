package dto

import (
	"fmt"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// --- Favorite stations -----------------------------------------------------

// FavoriteStationRequest is the body accepted by POST /api/stations/favorites.
type FavoriteStationRequest struct {
	Callsign string `json:"callsign"`
	Note     string `json:"note,omitempty"`
}

// Validate enforces addressee syntax and a non-empty callsign, mirroring
// BlockedCallsignRequest.Validate.
func (r FavoriteStationRequest) Validate() error {
	if strings.TrimSpace(r.Callsign) == "" {
		return fmt.Errorf("callsign is required")
	}
	if err := ValidateAddressee(r.Callsign); err != nil {
		return err
	}
	return nil
}

// ToModel builds a configstore row from the request. Callsign is
// uppercased by the model's BeforeSave hook; we upper here too so the
// unique-constraint collision check uses the canonical form.
func (r FavoriteStationRequest) ToModel() configstore.FavoriteStation {
	return configstore.FavoriteStation{
		Callsign: strings.ToUpper(strings.TrimSpace(r.Callsign)),
		Note:     r.Note,
	}
}

// FavoriteStationResponse is the body returned by GET/POST.
type FavoriteStationResponse struct {
	ID        uint32    `json:"id"`
	Callsign  string    `json:"callsign"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// FavoriteStationFromModel renders one row.
func FavoriteStationFromModel(m configstore.FavoriteStation) FavoriteStationResponse {
	return FavoriteStationResponse{
		ID:        m.ID,
		Callsign:  m.Callsign,
		Note:      m.Note,
		CreatedAt: m.CreatedAt.UTC(),
	}
}
