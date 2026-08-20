package dto

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

var bulletinGroupNameRe = regexp.MustCompile(`^[A-Z0-9]{0,5}$`)

// BulletinGroupRequest is the body accepted by POST /api/bulletins and
// PUT /api/bulletins/{id}.
type BulletinGroupRequest struct {
	Name        string  `json:"name"         example:"WX"`
	SendPath    string  `json:"send_path"    enums:"rf,both,is_only" example:"rf"`
	DigiPath    string  `json:"digi_path"    example:"WIDE1-1,WIDE2-1"`
	InitialRate int     `json:"initial_rate" example:"60"`   // seconds; min 30
	DecayFactor float64 `json:"decay_factor" example:"1.5"`
	StableRate  int     `json:"stable_rate"  example:"600"`  // seconds
	Active      bool    `json:"active"`
}

// Validate returns an error for any field that would cause the scheduler
// or wire format to misbehave.
func (r BulletinGroupRequest) Validate() error {
	upper := strings.ToUpper(strings.TrimSpace(r.Name))
	if !bulletinGroupNameRe.MatchString(upper) {
		return fmt.Errorf("name must be up to 5 characters (A-Z, 0-9)")
	}
	switch r.SendPath {
	case "", "rf", "both", "is_only":
	default:
		return fmt.Errorf("send_path must be one of rf, both, is_only (got %q)", r.SendPath)
	}
	if r.InitialRate < 30 {
		return fmt.Errorf("initial_rate must be at least 30 seconds")
	}
	if r.DecayFactor < 1.0 {
		return fmt.Errorf("decay_factor must be >= 1.0")
	}
	if r.StableRate < r.InitialRate {
		return fmt.Errorf("stable_rate must be >= initial_rate (%d)", r.InitialRate)
	}
	return nil
}

func (r BulletinGroupRequest) normalizedSendPath() string {
	switch r.SendPath {
	case "both", "is_only":
		return r.SendPath
	default:
		return "rf"
	}
}

// ToModel converts the request into a configstore.BulletinGroup.
// ID is left zero; the caller sets it for updates.
func (r BulletinGroupRequest) ToModel() configstore.BulletinGroup {
	return configstore.BulletinGroup{
		Name:        strings.ToUpper(strings.TrimSpace(r.Name)),
		SendPath:    r.normalizedSendPath(),
		DigiPath:    strings.TrimSpace(r.DigiPath),
		InitialRate: r.InitialRate,
		DecayFactor: r.DecayFactor,
		StableRate:  r.StableRate,
		Active:      r.Active,
	}
}

// BulletinItemRequest is the body accepted by PUT /api/bulletins/{id}/items/{slot}.
type BulletinItemRequest struct {
	Text   string `json:"text"`   // max 67 chars
	Active bool   `json:"active"`
}

// Validate ensures the bulletin item text fits within the APRS message limit.
func (r BulletinItemRequest) Validate() error {
	if len(r.Text) > 67 {
		return fmt.Errorf("bulletin text must be 67 characters or fewer (got %d)", len(r.Text))
	}
	return nil
}

// BulletinItemResponse is one slot returned inside BulletinGroupResponse.
type BulletinItemResponse struct {
	Slot      int    `json:"slot"`
	Text      string `json:"text"`
	Active    bool   `json:"active"`
	SendCount int    `json:"send_count"`
}

// BulletinGroupResponse is the outbound representation of a bulletin group.
type BulletinGroupResponse struct {
	ID          uint32                 `json:"id"`
	Name        string                 `json:"name"`
	SendPath    string                 `json:"send_path"`
	DigiPath    string                 `json:"digi_path"`
	InitialRate int                    `json:"initial_rate"`
	DecayFactor float64                `json:"decay_factor"`
	StableRate  int                    `json:"stable_rate"`
	Active      bool                   `json:"active"`
	Items       []BulletinItemResponse `json:"items"`
}

// BulletinGroupFromModel converts a configstore.BulletinGroup to the response DTO.
// Display name for Group with Name="" is returned as "" — the frontend
// substitutes "Global".
func BulletinGroupFromModel(g configstore.BulletinGroup) BulletinGroupResponse {
	items := make([]BulletinItemResponse, 0, len(g.Items))
	for _, it := range g.Items {
		items = append(items, BulletinItemResponse{
			Slot:      it.Slot,
			Text:      it.Text,
			Active:    it.Active,
			SendCount: it.SendCount,
		})
	}
	return BulletinGroupResponse{
		ID:          g.ID,
		Name:        g.Name,
		SendPath:    g.SendPath,
		DigiPath:    g.DigiPath,
		InitialRate: g.InitialRate,
		DecayFactor: g.DecayFactor,
		StableRate:  g.StableRate,
		Active:      g.Active,
		Items:       items,
	}
}
