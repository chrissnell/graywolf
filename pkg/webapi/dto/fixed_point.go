package dto

import (
	"fmt"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// FixedPointRequest is the body accepted by POST /api/fixed-points and
// PUT /api/fixed-points/{id}. A fixed point is a named map landmark with
// an APRS symbol; it is never transmitted.
type FixedPointRequest struct {
	Name        string  `json:"name"`
	SymbolTable string  `json:"symbol_table"`
	Symbol      string  `json:"symbol"`
	Overlay     string  `json:"overlay"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// Validate enforces a non-empty name and on-globe coordinates. Symbol
// fields are free-form (same vocabulary as station markers); the client
// SymbolPicker constrains them, so the server only range-checks coords.
func (r FixedPointRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if r.Latitude < -90 || r.Latitude > 90 {
		return fmt.Errorf("latitude out of range")
	}
	if r.Longitude < -180 || r.Longitude > 180 {
		return fmt.Errorf("longitude out of range")
	}
	return nil
}

// ToModel maps a create request to a storage model. Symbol defaults
// mirror the configstore column defaults so an omitted picker value
// still renders a visible marker.
func (r FixedPointRequest) ToModel() configstore.FixedPoint {
	table := r.SymbolTable
	if table == "" {
		table = "/"
	}
	symbol := r.Symbol
	if symbol == "" {
		symbol = "/"
	}
	return configstore.FixedPoint{
		Name:        strings.TrimSpace(r.Name),
		SymbolTable: table,
		Symbol:      symbol,
		Overlay:     r.Overlay,
		Latitude:    r.Latitude,
		Longitude:   r.Longitude,
	}
}

// ToUpdate maps an update request to a storage model, preserving id.
func (r FixedPointRequest) ToUpdate(id uint32) configstore.FixedPoint {
	m := r.ToModel()
	m.ID = id
	return m
}

// FixedPointResponse is the body returned by GET/POST/PUT for a fixed
// point.
type FixedPointResponse struct {
	ID uint32 `json:"id"`
	FixedPointRequest
}

// FixedPointFromModel converts a storage model into a response DTO.
func FixedPointFromModel(m configstore.FixedPoint) FixedPointResponse {
	return FixedPointResponse{
		ID: m.ID,
		FixedPointRequest: FixedPointRequest{
			Name:        m.Name,
			SymbolTable: m.SymbolTable,
			Symbol:      m.Symbol,
			Overlay:     m.Overlay,
			Latitude:    m.Latitude,
			Longitude:   m.Longitude,
		},
	}
}
