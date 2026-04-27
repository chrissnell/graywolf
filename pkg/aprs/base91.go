package aprs

// APRS base-91 encoding as used in compressed position, compressed
// telemetry, and various appendices. Each character is in the printable
// ASCII range '!' (33) to '{' (123), representing values 0..90.

// base91Decode4 decodes a 4-character base-91 number into an int32
// (matches the compressed-position YYYY/XXXX fields). Invalid bytes are
// silently clamped to zero to keep the parser total.
func base91Decode4(b []byte) int32 {
	if len(b) < 4 {
		return 0
	}
	var n int32
	for i := 0; i < 4; i++ {
		c := b[i]
		if c < '!' || c > '{' {
			return 0
		}
		n = n*91 + int32(c-'!')
	}
	return n
}

// base91DecodeN decodes n base-91 characters into an int64. Used by
// compressed telemetry (two-character base-91 channels).
func base91DecodeN(b []byte) int64 {
	var n int64
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c < '!' || c > '{' {
			return 0
		}
		n = n*91 + int64(c-'!')
	}
	return n
}

// base91Encode encodes n into a fixed-width w base-91 string. Used by
// the compressed-position encoder.
func base91Encode(n int64, w int) []byte {
	out := make([]byte, w)
	for i := w - 1; i >= 0; i-- {
		out[i] = byte(n%91) + '!'
		n /= 91
	}
	return out
}

// EncodeCompressedLatLon encodes a decimal latitude and longitude into
// the 4+4 character base-91 fields used by the compressed position
// report. Returns the concatenated 8 bytes YYYYXXXX.
func EncodeCompressedLatLon(lat, lon float64) []byte {
	y := int64((90.0 - lat) * 380926.0)
	if y < 0 {
		y = 0
	}
	if y > 91*91*91*91 {
		y = 91*91*91*91 - 1
	}
	x := int64((180.0 + lon) * 190463.0)
	if x < 0 {
		x = 0
	}
	if x > 91*91*91*91 {
		x = 91*91*91*91 - 1
	}
	out := make([]byte, 8)
	copy(out[0:4], base91Encode(y, 4))
	copy(out[4:8], base91Encode(x, 4))
	return out
}
