package dto

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// BulletinResponse is the REST representation of a bulletin row.
type BulletinResponse struct {
	ID             uint64     `json:"id"`
	Direction      string     `json:"direction"`
	Slot           string     `json:"slot"`
	FromCall       string     `json:"from_call"`
	Text           string     `json:"text"`
	Source         string     `json:"source,omitempty"`
	IsAnnouncement bool       `json:"is_announcement"`
	SendCount      uint32     `json:"send_count,omitempty"`
	MaxSends       uint32     `json:"max_sends,omitempty"`
	Unread         bool       `json:"unread"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	NextSendAt     *time.Time `json:"next_send_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// BulletinFromModel converts a configstore.Bulletin to a BulletinResponse.
func BulletinFromModel(b configstore.Bulletin) BulletinResponse {
	return BulletinResponse{
		ID:             b.ID,
		Direction:      b.Direction,
		Slot:           b.Slot,
		FromCall:       b.FromCall,
		Text:           b.Text,
		Source:         b.Source,
		IsAnnouncement: b.IsAnnouncement,
		SendCount:      b.SendCount,
		MaxSends:       b.MaxSends,
		Unread:         b.Unread,
		ExpiresAt:      b.ExpiresAt,
		NextSendAt:     b.NextSendAt,
		CreatedAt:      b.CreatedAt,
		UpdatedAt:      b.UpdatedAt,
	}
}

// SendBulletinRequest is the body for POST /api/bulletins.
type SendBulletinRequest struct {
	Slot string `json:"slot"`
	Text string `json:"text"`
}

// Validate returns an error if the request is not a valid bulletin.
func (r SendBulletinRequest) Validate() error {
	slot := strings.ToUpper(strings.TrimSpace(r.Slot))
	if !validBulletinSlot(slot) {
		return fmt.Errorf("slot %q is not a valid BLN0-9 or BLNA-Z identifier", r.Slot)
	}
	text := strings.TrimSpace(r.Text)
	if text == "" {
		return fmt.Errorf("text is required")
	}
	if len(text) > 67 {
		return fmt.Errorf("text too long (%d chars, max 67)", len(text))
	}
	return nil
}

func validBulletinSlot(slot string) bool {
	if len(slot) != 4 {
		return false
	}
	if slot[:3] != "BLN" {
		return false
	}
	c := rune(slot[3])
	return (c >= '0' && c <= '9') || (unicode.IsLetter(c) && unicode.IsUpper(c))
}
