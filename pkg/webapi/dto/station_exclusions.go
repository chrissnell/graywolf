package dto

import (
	"fmt"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// --- Excluded stations -------------------------------------------------------

// ExcludedStationRequest is the body accepted by POST /api/stations/exclusions.
type ExcludedStationRequest struct {
	Callsign string `json:"callsign"`
	Note     string `json:"note,omitempty"`
}

// Validate enforces addressee syntax and a non-empty callsign, mirroring
// FavoriteStationRequest.Validate.
func (r ExcludedStationRequest) Validate() error {
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
func (r ExcludedStationRequest) ToModel() configstore.ExcludedStation {
	return configstore.ExcludedStation{
		Callsign: strings.ToUpper(strings.TrimSpace(r.Callsign)),
		Note:     r.Note,
	}
}

// ExcludedStationResponse is the body returned by GET/POST.
type ExcludedStationResponse struct {
	ID        uint32    `json:"id"`
	Callsign  string    `json:"callsign"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ExcludedStationFromModel renders one row.
func ExcludedStationFromModel(m configstore.ExcludedStation) ExcludedStationResponse {
	return ExcludedStationResponse{
		ID:        m.ID,
		Callsign:  m.Callsign,
		Note:      m.Note,
		CreatedAt: m.CreatedAt.UTC(),
	}
}
