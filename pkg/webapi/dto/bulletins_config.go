package dto

import "fmt"

// BulletinsConfig is the on-wire shape of GET/PUT /api/bulletins/config.
type BulletinsConfig struct {
	TxChannel uint32 `json:"tx_channel"` // 0 = auto-resolve
	SendPath  string `json:"send_path"`  // rf | both | is_only
}

// Validate checks BulletinsConfig fields.
func (c BulletinsConfig) Validate() error {
	switch c.SendPath {
	case "", "rf", "both", "is_only":
	default:
		return fmt.Errorf("send_path must be one of rf, both, is_only (got %q)", c.SendPath)
	}
	return nil
}

// NormalizedSendPath returns "rf" for empty, otherwise the value as-is.
func (c BulletinsConfig) NormalizedSendPath() string {
	switch c.SendPath {
	case "both", "is_only":
		return c.SendPath
	default:
		return "rf"
	}
}
