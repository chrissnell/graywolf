// Package aprs parses and builds APRS (Automatic Packet Reporting System)
// packets carried in AX.25 UI frame information fields.
//
// The package covers the packet types graywolf needs to decode and
// transmit for normal 144.39 MHz operation:
//
//   - Position reports (!/=, @/`) uncompressed and compressed, with or
//     without timestamp
//   - Messages, bulletins, announcements, and NWS alerts (:)
//   - Telemetry (T#... and base-91 compressed form)
//   - Weather reports (_ positionless, @/` with weather appendix)
//   - Objects (;) and items ())
//   - Mic-E (' and `) with bit-packed latitude and manufacturer encoding
//   - Station capabilities (<IGATE,...>)
//   - Direction finding (DF reports with BRG/NRQ appendix)
//
// The parser is fuzz-friendly: every entry point checks bounds and
// returns an error rather than panicking on malformed input.
//
// Usage:
//
//	pkt, err := aprs.Parse(frame)   // frame is *ax25.Frame
//	if err != nil { ... }
//	if pkt.Position != nil {
//	    fmt.Println(pkt.Position.Latitude, pkt.Position.Longitude)
//	}
//
// Reference material: goballoon (position / message / telemetry / base91
// shapes were modernized from there), direwolf's decode_aprs.c and
// decode_mic_e.c (Mic-E bit layout), and the APRS Protocol Reference v1.0.1.
package aprs
