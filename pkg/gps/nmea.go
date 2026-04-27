package gps

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/metrics"
)

// Supported NMEA sentences: $GPRMC, $GPGGA, $GPVTG, $GPGSA, $GPGSV (and
// GN/GL/GA talker variants). Other sentences are silently ignored.

// validateAndSplit handles NMEA prefix, checksum verification, and field
// splitting common to all sentence parsers.
func validateAndSplit(line string) (tag string, fields []string, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil, errors.New("gps: empty nmea line")
	}
	if line[0] == '$' {
		line = line[1:]
	}
	body := line
	if i := strings.IndexByte(line, '*'); i >= 0 {
		body = line[:i]
		sum := line[i+1:]
		want, err := strconv.ParseUint(sum, 16, 8)
		if err != nil {
			return "", nil, fmt.Errorf("gps: bad checksum %q: %w", sum, err)
		}
		var got byte
		for j := 0; j < len(body); j++ {
			got ^= body[j]
		}
		if byte(want) != got {
			return "", nil, fmt.Errorf("gps: checksum mismatch want %02X got %02X", want, got)
		}
	}
	fields = strings.Split(body, ",")
	if len(fields) == 0 {
		return "", nil, errors.New("gps: no fields")
	}
	tag = fields[0]
	if len(tag) < 5 {
		return "", nil, fmt.Errorf("gps: short sentence tag %q", tag)
	}
	return tag, fields, nil
}

// ParseNMEA parses a single NMEA sentence into a Fix. The input may
// optionally include the leading '$' and trailing "*HH" checksum; both
// styles are accepted. Returns an error if the checksum is invalid or the
// sentence type is unsupported/malformed. The returned bool reports
// whether the sentence contained an active fix (some RMC sentences are
// status='V' for "void").
func ParseNMEA(line string) (Fix, bool, error) {
	tag, fields, err := validateAndSplit(line)
	if err != nil {
		return Fix{}, false, err
	}
	switch tag[2:] {
	case "RMC":
		return parseRMC(fields)
	case "GGA":
		return parseGGA(fields)
	case "VTG":
		return parseVTG(fields)
	case "GSA":
		return parseGSA(fields)
	case "GLL":
		return Fix{}, false, nil // subset of RMC; silently ignore
	default:
		return Fix{}, false, fmt.Errorf("gps: unsupported sentence %q", tag)
	}
}

// $xxRMC,hhmmss.ss,A,llll.ll,a,yyyyy.yy,a,x.x,x.x,ddmmyy,x.x,a*hh
//   1=time 2=status 3,4=lat 5,6=lon 7=speed kt 8=track 9=date
func parseRMC(f []string) (Fix, bool, error) {
	if len(f) < 10 {
		return Fix{}, false, fmt.Errorf("gps: RMC too short (%d fields)", len(f))
	}
	active := f[2] == "A"
	var fix Fix
	lat, err := parseLat(f[3], f[4])
	if err != nil {
		return Fix{}, false, err
	}
	lon, err := parseLon(f[5], f[6])
	if err != nil {
		return Fix{}, false, err
	}
	fix.Latitude = lat
	fix.Longitude = lon
	if f[7] != "" {
		if s, err := strconv.ParseFloat(f[7], 64); err == nil {
			fix.Speed = s
			fix.HasCourse = true
		}
	}
	if f[8] != "" {
		if h, err := strconv.ParseFloat(f[8], 64); err == nil {
			fix.Heading = h
			fix.HasCourse = true
		}
	}
	if f[1] != "" && f[9] != "" {
		if ts, err := parseNMEADateTime(f[9], f[1]); err == nil {
			fix.Timestamp = ts
		}
	}
	return fix, active, nil
}

// $xxGGA,hhmmss.ss,llll.ll,a,yyyyy.yy,a,q,nn,hh,altM,M,...
//   1=time 2,3=lat 4,5=lon 6=qual 7=sats 8=hdop 9=alt 10=alt units
func parseGGA(f []string) (Fix, bool, error) {
	if len(f) < 11 {
		return Fix{}, false, fmt.Errorf("gps: GGA too short (%d fields)", len(f))
	}
	var fix Fix
	qual := f[6]
	active := qual != "" && qual != "0"
	lat, err := parseLat(f[2], f[3])
	if err != nil {
		return Fix{}, false, err
	}
	lon, err := parseLon(f[4], f[5])
	if err != nil {
		return Fix{}, false, err
	}
	fix.Latitude = lat
	fix.Longitude = lon
	if f[9] != "" {
		if a, err := strconv.ParseFloat(f[9], 64); err == nil {
			fix.Altitude = a
			fix.HasAlt = true
		}
	}
	if f[1] != "" {
		if ts, err := parseNMEATimeOnly(f[1]); err == nil {
			fix.Timestamp = ts
		}
	}
	return fix, active, nil
}

// $xxVTG,cogt,T,cogm,M,sog_kn,N,sog_kmh,K,mode*hh
//   1=course true 3=course mag 5=speed knots 7=speed km/h 9=mode
func parseVTG(f []string) (Fix, bool, error) {
	if len(f) < 9 {
		return Fix{}, false, fmt.Errorf("gps: VTG too short (%d fields)", len(f))
	}
	// Mode 'N' = not valid.
	if len(f) > 9 && f[9] == "N" {
		return Fix{}, false, nil
	}
	var fix Fix
	if f[1] != "" {
		if h, err := strconv.ParseFloat(f[1], 64); err == nil {
			fix.Heading = h
			fix.HasCourse = true
		}
	}
	if f[5] != "" {
		if s, err := strconv.ParseFloat(f[5], 64); err == nil {
			fix.Speed = s
			fix.HasCourse = true
		}
	}
	return fix, fix.HasCourse, nil
}

// $xxGSA,mode,fix,prn1,...,prn12,pdop,hdop,vdop*hh
//   1=selection mode (A=auto,M=manual) 2=fix type (1=none,2=2D,3=3D)
//   3-14=satellite PRNs 15=PDOP 16=HDOP 17=VDOP
func parseGSA(f []string) (Fix, bool, error) {
	if len(f) < 18 {
		return Fix{}, false, fmt.Errorf("gps: GSA too short (%d fields)", len(f))
	}
	fixMode, _ := strconv.Atoi(f[2])
	if fixMode == 1 {
		return Fix{}, false, nil // no fix
	}
	var fix Fix
	fix.FixMode = fixMode
	if f[15] != "" {
		if v, err := strconv.ParseFloat(f[15], 64); err == nil {
			fix.PDOP = v
			fix.HasDOP = true
		}
	}
	if f[16] != "" {
		if v, err := strconv.ParseFloat(f[16], 64); err == nil {
			fix.HDOP = v
			fix.HasDOP = true
		}
	}
	if f[17] != "" {
		if v, err := strconv.ParseFloat(f[17], 64); err == nil {
			fix.VDOP = v
			fix.HasDOP = true
		}
	}
	return fix, fix.HasDOP, nil
}

// isGSVSentence quickly checks whether a raw NMEA line is a GSV sentence
// without performing full validation.
func isGSVSentence(line string) bool {
	s := strings.TrimSpace(line)
	if len(s) > 0 && s[0] == '$' {
		s = s[1:]
	}
	return len(s) >= 6 && s[2:5] == "GSV" && s[5] == ','
}

// parseGSVLine validates and parses a single GSV sentence, returning the
// talker ID (e.g. "GP"), message framing, and satellite entries.
func parseGSVLine(line string) (talker string, totalMsgs, msgNum int, sats []SatelliteInfo, err error) {
	tag, fields, err := validateAndSplit(line)
	if err != nil {
		return "", 0, 0, nil, err
	}
	if tag[2:] != "GSV" {
		return "", 0, 0, nil, fmt.Errorf("gps: not a GSV sentence: %q", tag)
	}
	totalMsgs, msgNum, sats, err = parseGSV(fields)
	return tag[:2], totalMsgs, msgNum, sats, err
}

// $xxGSV,totalMsgs,msgNum,totalSats,{prn,elev,azim,snr}*checksum
// Each message carries up to 4 satellites; a complete view spans
// totalMsgs consecutive messages.
func parseGSV(f []string) (totalMsgs, msgNum int, sats []SatelliteInfo, err error) {
	if len(f) < 4 {
		return 0, 0, nil, fmt.Errorf("gps: GSV too short (%d fields)", len(f))
	}
	totalMsgs, err = strconv.Atoi(f[1])
	if err != nil {
		return 0, 0, nil, fmt.Errorf("gps: GSV bad total messages: %w", err)
	}
	msgNum, err = strconv.Atoi(f[2])
	if err != nil {
		return 0, 0, nil, fmt.Errorf("gps: GSV bad message number: %w", err)
	}
	// f[3] = total satellites in view (informational, not stored)
	for i := 4; i+3 < len(f); i += 4 {
		if f[i] == "" {
			continue
		}
		prn, err := strconv.Atoi(f[i])
		if err != nil {
			continue
		}
		sat := SatelliteInfo{PRN: prn, SNR: -1}
		if f[i+1] != "" {
			sat.Elevation, _ = strconv.Atoi(f[i+1])
		}
		if f[i+2] != "" {
			sat.Azimuth, _ = strconv.Atoi(f[i+2])
		}
		if f[i+3] != "" {
			if snr, err := strconv.Atoi(f[i+3]); err == nil {
				sat.SNR = snr
			}
		}
		sats = append(sats, sat)
	}
	return totalMsgs, msgNum, sats, nil
}

// parseLat handles "DDMM.mmmm" with hemisphere letter.
func parseLat(val, hemi string) (float64, error) {
	if val == "" {
		return 0, nil
	}
	if len(val) < 4 {
		return 0, fmt.Errorf("gps: short lat %q", val)
	}
	deg, err := strconv.ParseFloat(val[:2], 64)
	if err != nil {
		return 0, err
	}
	min, err := strconv.ParseFloat(val[2:], 64)
	if err != nil {
		return 0, err
	}
	d := deg + min/60.0
	if hemi == "S" {
		d = -d
	}
	return d, nil
}

// parseLon handles "DDDMM.mmmm" with hemisphere letter.
func parseLon(val, hemi string) (float64, error) {
	if val == "" {
		return 0, nil
	}
	if len(val) < 5 {
		return 0, fmt.Errorf("gps: short lon %q", val)
	}
	deg, err := strconv.ParseFloat(val[:3], 64)
	if err != nil {
		return 0, err
	}
	min, err := strconv.ParseFloat(val[3:], 64)
	if err != nil {
		return 0, err
	}
	d := deg + min/60.0
	if hemi == "W" {
		d = -d
	}
	return d, nil
}

func parseNMEADateTime(date, tod string) (time.Time, error) {
	if len(date) != 6 || len(tod) < 6 {
		return time.Time{}, fmt.Errorf("gps: bad date/time %q %q", date, tod)
	}
	dd, _ := strconv.Atoi(date[0:2])
	mm, _ := strconv.Atoi(date[2:4])
	yy, _ := strconv.Atoi(date[4:6])
	year := 2000 + yy
	hh, _ := strconv.Atoi(tod[0:2])
	mi, _ := strconv.Atoi(tod[2:4])
	ss, err := strconv.ParseFloat(tod[4:], 64)
	if err != nil {
		return time.Time{}, err
	}
	nsec := int((ss - float64(int(ss))) * 1e9)
	return time.Date(year, time.Month(mm), dd, hh, mi, int(ss), nsec, time.UTC), nil
}

func parseNMEATimeOnly(tod string) (time.Time, error) {
	if len(tod) < 6 {
		return time.Time{}, fmt.Errorf("gps: bad time %q", tod)
	}
	now := time.Now().UTC()
	hh, _ := strconv.Atoi(tod[0:2])
	mi, _ := strconv.Atoi(tod[2:4])
	ss, err := strconv.ParseFloat(tod[4:], 64)
	if err != nil {
		return time.Time{}, err
	}
	nsec := int((ss - float64(int(ss))) * 1e9)
	return time.Date(now.Year(), now.Month(), now.Day(), hh, mi, int(ss), nsec, time.UTC), nil
}

// NMEAOptions configures ReadNMEAStream. OnParseError is optional and,
// when non-nil, is invoked once per malformed sentence — wired to the
// shared gps parse-errors counter in production. Kept as a separate
// option struct so adding more knobs later doesn't require a breaking
// signature change on every caller.
type NMEAOptions struct {
	OnParseError func(source string)
}

// ReadNMEAStream consumes NMEA sentences from r line-by-line, parses them,
// and pushes accepted fixes into cache. It handles partial lines across
// reads (bufio.Scanner) and logs malformed sentences at debug level, with
// a 1-minute rate-limited warn log for parse failures so an operator sees
// the first one of each surge without the log flooding. It returns when
// ctx is cancelled or r hits EOF.
func ReadNMEAStream(ctx context.Context, r io.Reader, cache PositionCache, logger *slog.Logger, opts NMEAOptions) error {
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "gps/nmea")
	scanner := bufio.NewScanner(r)
	// NMEA sentences are at most 82 bytes, but some receivers emit longer
	// proprietary ones; allow up to 4 KiB per line.
	scanner.Buffer(make([]byte, 0, 4096), 4096)

	parseErrLog := metrics.NewRateLimitedLogger(time.Minute)

	var (
		lines     int
		fixes     int
		voids     int
		parseErrs int
		firstLine = true
		lastStats = time.Now()
	)
	statsInterval := 10 * time.Second

	// GSV accumulation: each talker (GP, GL, GA, GB) sends its own
	// numbered message cycle; we collect per-talker and merge into a
	// single SatelliteView when any cycle completes.
	gsvPartial := make(map[string][]SatelliteInfo)
	gsvComplete := make(map[string][]SatelliteInfo)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line := scanner.Text()
		lines++
		if firstLine {
			logger.Info("gps: first line received from device", "line", line)
			firstLine = false
		}
		// GSV sentences carry satellite visibility, not position fixes.
		if isGSVSentence(line) {
			talker, totalMsgs, msgNum, sats, err := parseGSVLine(line)
			if err != nil {
				parseErrs++
				if opts.OnParseError != nil {
					opts.OnParseError("nmea")
				}
				logger.Debug("nmea GSV parse error", "err", err, "line", line)
				snippet := line
				if len(snippet) > 80 {
					snippet = snippet[:80]
				}
				parseErrLog.Log(logger, slog.LevelWarn, "parse",
					"nmea parse error",
					"err", err, "snippet", snippet)
				continue
			}
			if msgNum == 1 {
				gsvPartial[talker] = sats
			} else {
				gsvPartial[talker] = append(gsvPartial[talker], sats...)
			}
			if msgNum == totalMsgs {
				gsvComplete[talker] = gsvPartial[talker]
				delete(gsvPartial, talker)
				if sc, ok := cache.(SatelliteCache); ok {
					var all []SatelliteInfo
					for _, s := range gsvComplete {
						all = append(all, s...)
					}
					sc.UpdateSatellites(SatelliteView{
						Satellites: all,
						UpdatedAt:  time.Now().UTC(),
					})
				}
			}
			continue
		}
		fix, active, err := ParseNMEA(line)
		if err != nil {
			parseErrs++
			if opts.OnParseError != nil {
				opts.OnParseError("nmea")
			}
			logger.Debug("nmea parse error", "err", err, "line", line)
			snippet := line
			if len(snippet) > 80 {
				snippet = snippet[:80]
			}
			parseErrLog.Log(logger, slog.LevelWarn, "parse",
				"nmea parse error",
				"err", err, "snippet", snippet)
		} else if !active {
			voids++
			logger.Debug("nmea void/no-fix sentence", "line", line)
		} else if fix.Latitude == 0 && fix.Longitude == 0 && fix.HasDOP {
			// DOP-only sentence (GSA): merge DOP/fix mode into last fix.
			if prev, ok := cache.Get(); ok {
				prev.FixMode = fix.FixMode
				prev.PDOP = fix.PDOP
				prev.HDOP = fix.HDOP
				prev.VDOP = fix.VDOP
				prev.HasDOP = true
				cache.Update(prev)
			}
		} else if fix.Latitude == 0 && fix.Longitude == 0 && fix.HasCourse {
			// Course-only sentence (VTG): merge speed/heading into last fix.
			if prev, ok := cache.Get(); ok {
				prev.Speed = fix.Speed
				prev.Heading = fix.Heading
				prev.HasCourse = true
				cache.Update(prev)
			}
		} else {
			fixes++
			cache.Update(fix)
			logger.Debug("nmea fix accepted",
				"lat", fix.Latitude, "lon", fix.Longitude,
				"alt", fix.Altitude, "speed_kt", fix.Speed)
		}
		if time.Since(lastStats) >= statsInterval {
			logger.Debug("gps stream stats",
				"lines", lines, "fixes", fixes, "voids", voids, "parse_errs", parseErrs)
			lastStats = time.Now()
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		logger.Warn("gps stream ended with error",
			"err", err, "lines", lines, "fixes", fixes, "voids", voids, "parse_errs", parseErrs)
		return err
	}
	logger.Info("gps stream ended",
		"lines", lines, "fixes", fixes, "voids", voids, "parse_errs", parseErrs)
	return nil
}
