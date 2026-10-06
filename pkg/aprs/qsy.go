package aprs

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// QSY is a decoded APRS "frequency specification" (AFRS informal
// extension, see aprs.org/info/freqspec.txt) parsed from the leading
// bytes of a packet's comment field, e.g. "146.520MHz T100 +060".
//
// v1 scope covers only this leading-comment form. FREQ-as-object-name,
// D-STAR, and NMEA status-beacon freq forms are not parsed. The
// "oXXX" alternate offset spelling and range tokens (Rxxm/Rxxk) are
// tolerated in the trailing text but not decoded/stored.
type QSY struct {
	FrequencyMHz float64
	ToneType     string // "ctcss" | "dcs" | ""
	ToneFreq     string // CTCSS: decimal Hz string (e.g. "100.0"); DCS: 3-digit code (e.g. "023")
	OffsetMHz    float64
	HasOffset    bool // distinguishes an explicit offset (incl. 0 = forced simplex) from "no offset token present"
}

// ctcssTones is the standard EIA CTCSS tone set in Hz. The wire format
// truncates a tone to its integer part (e.g. 107.2 -> "T107"), which is
// lossy; ctcssByInt below reverses that truncation.
var ctcssTones = []float64{
	67.0, 69.3, 71.9, 74.4, 77.0, 79.7, 82.5, 85.4, 88.5, 91.5,
	94.8, 97.4, 100.0, 103.5, 107.2, 110.9, 114.8, 118.8, 123.0, 127.3,
	131.8, 136.5, 141.3, 146.2, 151.4, 156.7, 159.8, 162.2, 165.5, 167.9,
	171.3, 173.8, 177.3, 179.9, 183.5, 186.2, 189.9, 192.8, 196.6, 199.5,
	203.5, 206.5, 210.7, 218.1, 225.7, 229.1, 233.6, 241.8, 250.3, 254.1,
}

// ctcssByInt maps the truncated integer part of a standard tone to its
// full decimal value, e.g. 107 -> 107.2.
var ctcssByInt = func() map[int]float64 {
	m := make(map[int]float64, len(ctcssTones))
	for _, t := range ctcssTones {
		m[int(t)] = t
	}
	return m
}()

// qsyRE matches a leading AFRS frequency spec: "FFF.FF MHz" or
// "FFF.FFFMHz", optionally followed by a tone token (Tnnn/Cnnn/Dnnn,
// case-insensitive — lowercase "narrow modulation" variants are
// accepted but that distinction is not decoded) and/or a signed
// 3-digit offset token in tens-of-kHz.
var qsyRE = regexp.MustCompile(`^ ?(\d{3}\.\d{2,3}) ?[Mm][Hh][Zz](?: ([TCDtcd])(\d{3}))?(?: ([+-]\d{3}))?`)

// ParseQSY scans the leading bytes of comment for an AFRS frequency
// specification. On a match it returns the decoded *QSY and the
// remaining comment text with the matched prefix (and one trailing
// separator space) stripped. On no match it returns (nil, comment)
// unchanged.
func ParseQSY(comment string) (*QSY, string) {
	m := qsyRE.FindStringSubmatch(comment)
	if m == nil {
		return nil, comment
	}
	freq, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil, comment
	}
	q := &QSY{FrequencyMHz: freq}
	if m[2] != "" {
		switch strings.ToUpper(m[2]) {
		case "T", "C":
			q.ToneType = "ctcss"
			code, _ := strconv.Atoi(m[3])
			if tone, ok := ctcssByInt[code]; ok {
				q.ToneFreq = strconv.FormatFloat(tone, 'f', 1, 64)
			} else {
				q.ToneFreq = fmt.Sprintf("%d.0", code)
			}
		case "D":
			q.ToneType = "dcs"
			q.ToneFreq = m[3]
		}
	}
	if m[4] != "" {
		if tenths, err := strconv.Atoi(m[4]); err == nil {
			q.OffsetMHz = float64(tenths) / 100
			q.HasOffset = true
		}
	}
	rest := strings.TrimPrefix(comment[len(m[0]):], " ")
	return q, rest
}

// EncodeQSY builds the AFRS frequency-spec prefix for q, e.g.
// "146.520MHz T100 +060". Returns "" for a nil q or non-positive
// frequency. The tone value is truncated to its integer part per spec
// (lossy by design; CTCSS 107.2 encodes as "T107").
func EncodeQSY(q *QSY) string {
	if q == nil || q.FrequencyMHz <= 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%.3fMHz", q.FrequencyMHz)
	switch q.ToneType {
	case "ctcss":
		if tone, err := strconv.ParseFloat(q.ToneFreq, 64); err == nil {
			fmt.Fprintf(&sb, " T%03d", int(math.Floor(tone)))
		}
	case "dcs":
		if code, err := strconv.Atoi(q.ToneFreq); err == nil {
			fmt.Fprintf(&sb, " D%03d", code)
		}
	}
	if q.HasOffset {
		tenths := int(math.Round(q.OffsetMHz * 100))
		sign := byte('+')
		if tenths < 0 {
			sign = '-'
			tenths = -tenths
		}
		fmt.Fprintf(&sb, " %c%03d", sign, tenths)
	}
	return sb.String()
}
