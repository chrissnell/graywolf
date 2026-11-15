package configstore

import "fmt"

// ValidateFX25 rejects settings the native FX.25 modem cannot honor.
func (c Channel) ValidateFX25() error {
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
