package aprs

import "strings"

// FormatTNC2 renders a TNC-2 wire line: source>dest[,path...]:info.
//
// Used by both originated traffic (locally-built APRS-IS submissions
// such as outbound messages or beacons) and gated traffic (RF packets
// re-emitted to APRS-IS). Each caller owns the path and info bytes;
// this helper only owns the structural glue:
//
//   - the `>` between source and dest
//   - the `,` between path elements
//   - the `:` between the path field and the info field
//
// The path-terminator `:` is the load-bearing detail. APRS-IS consumers
// (aprs.fi and the like) reject lines missing it as "Unsupported packet
// format" — a class of bug that bit graywolf in May 2026 when the
// originating-side encoder was emitting only a single colon. Both
// encoders going through this helper makes that recurrence impossible.
//
// Empty path elements are silently dropped. The info bytes are written
// verbatim — the caller is responsible for any APRS data-type
// identifier that has to lead them (`:` for messages, `!` for position,
// etc.).
func FormatTNC2(source, dest string, path []string, info []byte) string {
	var b strings.Builder
	b.Grow(len(source) + 1 + len(dest) + 16 + len(info))
	b.WriteString(source)
	b.WriteByte('>')
	b.WriteString(dest)
	for _, p := range path {
		if p == "" {
			continue
		}
		b.WriteByte(',')
		b.WriteString(p)
	}
	b.WriteByte(':')
	b.Write(info)
	return b.String()
}
