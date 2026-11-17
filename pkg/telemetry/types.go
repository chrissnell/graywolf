// Package telemetry reports bounded, station-wide ten-minute APRS activity.
// It owns no radio or network connection; the application supplies counters and transports.
package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/callsign"
)

const Window = 10 * time.Minute
const SampleEvery = 10 * time.Second
const DefinitionSpacing = 30 * time.Second
const MaxQueueAge = 2 * time.Minute

// Config is one reporter's configuration. Channel selects delivery, never measurements.
type Config struct {
	Enabled           bool   `json:"enabled"`
	Callsign          string `json:"callsign"` // Empty inherits the current station identity.
	Channel           uint32 `json:"channel"`
	Delivery          string `json:"delivery"` // auto, aprsis, rf
	RFPath            string `json:"rf_path"`
	Extended          bool   `json:"allow_above_255"`
	MetadataISSeconds uint32 `json:"metadata_is_seconds"`
	MetadataRFSeconds uint32 `json:"metadata_rf_seconds"`
}

func Defaults() Config {
	return Config{Delivery: "auto", MetadataISSeconds: 7200, MetadataRFSeconds: 21600}
}
func Normalize(c Config) Config {
	c.Callsign = strings.ToUpper(strings.TrimSpace(c.Callsign))
	c.RFPath = strings.ToUpper(strings.TrimSpace(c.RFPath))
	if c.Delivery == "" {
		c.Delivery = "auto"
	}
	if c.MetadataISSeconds == 0 {
		c.MetadataISSeconds = 7200
	}
	if c.MetadataRFSeconds == 0 {
		c.MetadataRFSeconds = 21600
	}
	return c
}
func Validate(c Config) error {
	if c.Delivery != "auto" && c.Delivery != "aprsis" && c.Delivery != "rf" {
		return fmt.Errorf("delivery must be auto, aprsis or rf")
	}
	if c.Callsign != "" {
		if err := ValidateIdentity(c.Callsign); err != nil {
			return err
		}
	}
	if via, err := ax25.ParseVia(c.RFPath); err != nil {
		return fmt.Errorf("RF path: %w", err)
	} else if len(via) > 8 {
		return fmt.Errorf("RF path may contain at most eight addresses")
	}
	for _, seconds := range []uint32{c.MetadataISSeconds, c.MetadataRFSeconds} {
		if seconds < 3600 || seconds > 604800 {
			return fmt.Errorf("definition refresh must be 3600-604800 seconds")
		}
	}
	if c.Enabled && c.Delivery != "aprsis" && c.Channel == 0 {
		return fmt.Errorf("select an RF delivery channel")
	}
	return nil
}
func ValidateIdentity(call string) error {
	if call == "" || callsign.IsN0Call(call) {
		return fmt.Errorf("configure a real telemetry or station callsign")
	}
	if _, err := ax25.ParseAddress(call); err != nil {
		return fmt.Errorf("telemetry callsign: %w", err)
	}
	return nil
}

// Counts order is a wire contract: RF Rx, Digi, RF Tx, RF -> IS, IS -> RF.
type Counts [5]uint64

var Labels = [5]string{"RF Rx", "Digi", "RF Tx", "RF > IS", "IS > RF"}

type Observation struct {
	Counts          Counts
	DigiSource      string
	StationCallsign string
	InputReady      bool
	ISReady         bool
	RFReady         bool
}

// State is durable: no samples or per-packet history are written to disk.
type State struct {
	NextSequence     uint32               `json:"next_sequence"`
	MetadataIdentity string               `json:"metadata_identity"`
	Metadata         map[string]time.Time `json:"metadata"`
}

// Report records a local data submission, not a remote delivery acknowledgement.
type Report struct {
	SubmittedAt time.Time `json:"submitted_at"`
	Identity    string    `json:"identity"`
	Route       string    `json:"route"`
	Counts      Counts    `json:"counts"`
	Encoded     Counts    `json:"encoded"`
	Clipped     bool      `json:"clipped"`
	Payload     string    `json:"payload"`
}

type Status struct {
	Recent            []Report  `json:"recent"`
	State             string    `json:"state"`
	Scope             string    `json:"scope"`
	EffectiveCallsign string    `json:"effective_callsign"`
	DigiSource        string    `json:"digi_counter_source"`
	InputReady        bool      `json:"input_ready"`
	Route             string    `json:"route"`
	WarmupSeconds     int       `json:"warmup_seconds"`
	Ready             bool      `json:"ready"`
	Counts            Counts    `json:"counts"`
	Encoded           Counts    `json:"encoded"`
	Clipped           bool      `json:"clipped"`
	Preview           string    `json:"preview"`
	LastPayload       string    `json:"last_payload"`
	LastSubmission    time.Time `json:"last_submission"`
	NextReport        time.Time `json:"next_report"`
	LastError         string    `json:"last_error"`
}

type Options struct {
	Cancel  func() // Invalidates previously queued RF work on reset/reconfiguration.
	Load    func(context.Context) (Config, State, error)
	Save    func(context.Context, State) error
	Observe func(context.Context, Config) (Observation, error)
	// Send must preserve queue order, bound radio queue age, and never retry on another route.
	Send func(context.Context, Config, string, string, string, string) error // identity, route, kind, info
}
