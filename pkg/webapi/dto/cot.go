package dto

import (
	"fmt"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/cot"
)

// CotSettingsRequest is the body accepted by PUT /api/cot-settings --
// the global parameters every new Cursor-on-Target (CoT) object
// snapshots onto itself at creation time (see configstore.CotSettings
// and configstore.CotTarget).
type CotSettingsRequest struct {
	// Type is reserved for a future non-object CoT kind; only "object"
	// validates today.
	Type                 string  `json:"type" enums:"object" example:"object"`
	SendPath             string  `json:"send_path" enums:"rf,both,is_only" example:"rf"`
	Channel              uint32  `json:"channel"`
	Destination          string  `json:"destination" example:"APGRWO"`
	Path                 string  `json:"path" example:"WIDE1-1,WIDE2-1"`
	NumTransmits         uint32  `json:"num_transmits" example:"4"`
	SecondTxDelaySeconds uint32  `json:"second_tx_delay_seconds" example:"300"`
	DecayFactor          float64 `json:"decay_factor" example:"2"`
}

// Validate mirrors BeaconRequest's send_path enum check and adds the
// interval-math guards ComputeOffsets needs to stay well-defined.
func (r CotSettingsRequest) Validate() error {
	if r.Type != "object" {
		return fmt.Errorf(`type must be "object"`)
	}
	switch r.SendPath {
	case beacon.SendPathRF, beacon.SendPathBoth, beacon.SendPathISOnly:
	default:
		return fmt.Errorf("send_path must be one of rf, both, is_only (got %q)", r.SendPath)
	}
	if strings.TrimSpace(r.Destination) == "" {
		return fmt.Errorf("destination is required")
	}
	if r.NumTransmits < 1 {
		return fmt.Errorf("num_transmits must be at least 1")
	}
	if r.SecondTxDelaySeconds < 1 {
		return fmt.Errorf("second_tx_delay_seconds must be at least 1")
	}
	if r.DecayFactor <= 0 {
		return fmt.Errorf("decay_factor must be greater than 0")
	}
	return nil
}

// ToModel maps a validated request into the storage singleton.
func (r CotSettingsRequest) ToModel() configstore.CotSettings {
	return configstore.CotSettings{
		Type:                 r.Type,
		SendPath:             r.SendPath,
		Channel:              r.Channel,
		Destination:          strings.TrimSpace(r.Destination),
		Path:                 r.Path,
		NumTransmits:         r.NumTransmits,
		SecondTxDelaySeconds: r.SecondTxDelaySeconds,
		DecayFactor:          r.DecayFactor,
	}
}

// CotSettingsResponse is the body returned by GET/PUT /api/cot-settings.
type CotSettingsResponse struct {
	Type                 string  `json:"type"`
	SendPath             string  `json:"send_path"`
	Channel              uint32  `json:"channel"`
	Destination          string  `json:"destination"`
	Path                 string  `json:"path"`
	NumTransmits         uint32  `json:"num_transmits"`
	SecondTxDelaySeconds uint32  `json:"second_tx_delay_seconds"`
	DecayFactor          float64 `json:"decay_factor"`
}

// CotSettingsFromModel converts the persisted singleton into the
// response DTO.
func CotSettingsFromModel(m configstore.CotSettings) CotSettingsResponse {
	return CotSettingsResponse{
		Type:                 m.Type,
		SendPath:             m.SendPath,
		Channel:              m.Channel,
		Destination:          m.Destination,
		Path:                 m.Path,
		NumTransmits:         m.NumTransmits,
		SecondTxDelaySeconds: m.SecondTxDelaySeconds,
		DecayFactor:          m.DecayFactor,
	}
}

// CotSettingsDefaults returns the response DTO populated from
// cot.DefaultSettings(), the single source of truth for CoT settings
// defaults. GET /api/cot-settings uses this on a fresh install where no
// singleton row has been written yet.
func CotSettingsDefaults() CotSettingsResponse {
	return CotSettingsFromModel(cot.DefaultSettings())
}

// CotTargetRequest is the body accepted by POST /api/cot-targets. Only
// the operator-authored fields live here -- channel/destination/path/
// send-path/schedule all come from the current CotSettings singleton,
// snapshotted onto the row by ToModel.
type CotTargetRequest struct {
	ObjectName  string  `json:"object_name" example:"WOOFWOOF"`
	SymbolTable string  `json:"symbol_table" example:"/"`
	Symbol      string  `json:"symbol" example:"D"`
	Overlay     string  `json:"overlay"`
	Comment     string  `json:"comment"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// Validate enforces the same object-name length limit as beacon object
// beacons (APRS101 ch 11: 9 characters) and requires a comment, since
// the "Add CoT" dialog only enables Send once both are non-empty.
func (r CotTargetRequest) Validate() error {
	name := strings.TrimSpace(r.ObjectName)
	if name == "" {
		return fmt.Errorf("object_name is required")
	}
	if len(name) > 9 {
		return fmt.Errorf("object_name must be 9 characters or fewer")
	}
	if strings.TrimSpace(r.Comment) == "" {
		return fmt.Errorf("comment is required")
	}
	if r.Latitude < -90 || r.Latitude > 90 {
		return fmt.Errorf("latitude out of range")
	}
	if r.Longitude < -180 || r.Longitude > 180 {
		return fmt.Errorf("longitude out of range")
	}
	return nil
}

// ToModel maps the request plus a settings snapshot into a storage
// row. Symbol table/code default to "/"+"D" (the confirmed default
// icon) when omitted.
func (r CotTargetRequest) ToModel(settings configstore.CotSettings) configstore.CotTarget {
	table := r.SymbolTable
	if table == "" {
		table = "/"
	}
	symbol := r.Symbol
	if symbol == "" {
		symbol = "D"
	}
	return configstore.CotTarget{
		ObjectName:  strings.TrimSpace(r.ObjectName),
		SymbolTable: table,
		Symbol:      symbol,
		Overlay:     r.Overlay,
		Comment:     strings.TrimSpace(r.Comment),
		Latitude:    r.Latitude,
		Longitude:   r.Longitude,

		Type:                 settings.Type,
		SendPath:             settings.SendPath,
		Channel:              settings.Channel,
		Destination:          settings.Destination,
		Path:                 settings.Path,
		NumTransmits:         settings.NumTransmits,
		SecondTxDelaySeconds: settings.SecondTxDelaySeconds,
		DecayFactor:          settings.DecayFactor,
	}
}

// CotTargetResponse is the body returned for a CoT target. Active is
// computed at mapping time (TxCount < NumTransmits) rather than
// stored -- it's what the frontend uses to split the Active/Inactive
// Cursor-on-Targets tabs on the Beacons page.
type CotTargetResponse struct {
	ID                   uint32     `json:"id"`
	ObjectName           string     `json:"object_name"`
	SymbolTable          string     `json:"symbol_table"`
	Symbol               string     `json:"symbol"`
	Overlay              string     `json:"overlay"`
	Comment              string     `json:"comment"`
	Latitude             float64    `json:"latitude"`
	Longitude            float64    `json:"longitude"`
	Type                 string     `json:"type"`
	SendPath             string     `json:"send_path"`
	Channel              uint32     `json:"channel"`
	Destination          string     `json:"destination"`
	Path                 string     `json:"path"`
	NumTransmits         uint32     `json:"num_transmits"`
	SecondTxDelaySeconds uint32     `json:"second_tx_delay_seconds"`
	DecayFactor          float64    `json:"decay_factor"`
	TxCount              uint32     `json:"tx_count"`
	Active               bool       `json:"active"`
	FirstSentAt          *time.Time `json:"first_sent_at,omitempty"`
	NextSendAt           *time.Time `json:"next_send_at,omitempty"`
	LastSentAt           *time.Time `json:"last_sent_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

// CotTargetFromModel converts a storage row into the response DTO.
func CotTargetFromModel(m configstore.CotTarget) CotTargetResponse {
	return CotTargetResponse{
		ID:                   m.ID,
		ObjectName:           m.ObjectName,
		SymbolTable:          m.SymbolTable,
		Symbol:               m.Symbol,
		Overlay:              m.Overlay,
		Comment:              m.Comment,
		Latitude:             m.Latitude,
		Longitude:            m.Longitude,
		Type:                 m.Type,
		SendPath:             m.SendPath,
		Channel:              m.Channel,
		Destination:          m.Destination,
		Path:                 m.Path,
		NumTransmits:         m.NumTransmits,
		SecondTxDelaySeconds: m.SecondTxDelaySeconds,
		DecayFactor:          m.DecayFactor,
		TxCount:              m.TxCount,
		Active:               m.TxCount < m.NumTransmits,
		FirstSentAt:          m.FirstSentAt,
		NextSendAt:           m.NextSendAt,
		LastSentAt:           m.LastSentAt,
		CreatedAt:            m.CreatedAt,
	}
}

// CotSendResponse is the body returned by POST /api/cot-targets/{id}/send.
type CotSendResponse struct {
	Status string `json:"status" example:"sent"`
}

// CotDeleteInactiveResponse is the body returned by DELETE
// /api/cot-targets/inactive.
type CotDeleteInactiveResponse struct {
	Deleted int64 `json:"deleted" example:"3"`
}
