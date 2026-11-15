package modembridge

import "fmt"

// validateFX25Frame preflights the protected HDLC size before queueing IPC.
// The Rust encoder repeats this check. Flags, stuffed FCS and stuffed data
// must all fit the largest standard information field (239 bytes).
func validateFX25Frame(frame []byte) error {
	if len(frame) < 15 || len(frame) > 235 {
		return fmt.Errorf("FX.25 requires 15..235 AX.25 bytes before FCS; stuffing may reduce the maximum")
	}
	crc := uint16(0xffff)
	for _, b := range frame {
		crc ^= uint16(b)
		for n := 0; n < 8; n++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	crc = ^crc
	bits, ones := 16, 0 // opening and closing flags
	count := func(b byte) {
		for n := 0; n < 8; n++ {
			bits++
			if b&(1<<n) != 0 {
				ones++
				if ones == 5 {
					bits++
					ones = 0
				}
			} else {
				ones = 0
			}
		}
	}
	for _, b := range frame {
		count(b)
	}
	count(byte(crc))
	count(byte(crc >> 8))
	if bits > 239*8 {
		return fmt.Errorf("FX.25 frame needs %d protected bits; maximum is 1912 (239 bytes)", bits)
	}
	return nil
}
