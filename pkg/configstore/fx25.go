package configstore

import "fmt"

// ValidateFX25 rejects settings the native FX.25 modem cannot honor.
func (c Channel) ValidateFX25() error {
	switch c.FX25Mode {
	case "", "auto", "16", "32", "64":
	default:
		return fmt.Errorf("FX.25 mode must be auto, 16, 32, or 64")
	}
	if !c.FX25Encode {
		return nil
	}
	if c.InputDeviceID == nil || c.ModemType != "afsk" || c.BitRate != 1200 || c.MarkFreq != 1200 || c.SpaceFreq != 2200 {
		return fmt.Errorf("FX.25 requires a native 1200-baud AFSK channel with 1200/2200 Hz tones")
	}
	if c.IL2PEncode {
		return fmt.Errorf("FX.25 and IL2P encoding cannot both be enabled")
	}
	return nil
}

// FX25ParityBytes maps the explicit public mode to the internal modem enum.
// ValidateFX25 must run first. Zero here means automatic, never disabled;
// FX25Encode is the independent transmit enable switch.
func (c Channel) FX25ParityBytes() uint32 {
	switch c.FX25Mode {
	case "16":
		return 16
	case "32":
		return 32
	case "64":
		return 64
	default:
		return 0
	}
}
