package aprs

// Raw-GPS NMEA sentence decoding (APRS101 ch 5 "Raw GPS Data", prefix
// '$'). APRS permits a handful of NMEA-0183 sentences to be sent
// verbatim; this file handles $GPRMC (recommended minimum, time + fix
// + position + speed + course) which is the only form in the FAP test
// corpus. $GPGGA and $GPGLL would slot in alongside it.
//
// Sentence layout:
//   $GPRMC,hhmmss,A,ddmm.mmmm,N,dddmm.mmmm,W,sss.sss,ccc.c,ddmmyy,mmm.m,E*HH
//          time   ok lat      h  lon       h  speedKn course date   magvar* cksum

import (
	"errors"
	"strconv"
	"strings"
)

// parseNMEA is invoked when info starts with '$'. It validates the
// NMEA checksum, splits the sentence, and dispatches on the tag.
func parseNMEA(pkt *DecodedAPRSPacket, info []byte) error {
	// Peet Bros Ultimeter $ULTW… — distinct framing, no NMEA checksum.
	if len(info) >= 5 && string(info[:5]) == "$ULTW" {
		return parseUltwDollar(pkt, info)
	}
	s := string(info)
	// Require at least "$GPRMC,...*HH".
	star := strings.LastIndexByte(s, '*')
	if star < 0 || star+3 > len(s) {
		return errors.New("aprs: nmea missing checksum")
	}
	body := s[1:star] // between '$' and '*'
	sum := s[star+1 : star+3]
	var computed byte
	for i := 0; i < len(body); i++ {
		computed ^= body[i]
	}
	want, err := strconv.ParseUint(sum, 16, 8)
	if err != nil || byte(want) != computed {
		return errors.New("aprs: nmea bad checksum")
	}
	fields := strings.Split(body, ",")
	if len(fields) == 0 {
		return errors.New("aprs: nmea empty")
	}
	switch fields[0] {
	case "GPRMC":
		return parseGPRMC(pkt, fields)
	}
	// Unknown NMEA sentence — leave as unknown packet with raw text.
	pkt.Type = PacketUnknown
	pkt.Comment = s
	return nil
}

// parseGPRMC handles the "recommended minimum" sentence. Required
// fields: time(1), status(2), lat(3), N/S(4), lon(5), E/W(6), speed
// in knots(7), course(8), date(9).
func parseGPRMC(pkt *DecodedAPRSPacket, f []string) error {
	if len(f) < 10 {
		return errors.New("aprs: gprmc field count")
	}
	if f[2] != "A" {
		return errors.New("aprs: gprmc void fix")
	}
	lat, err := nmeaLatLon(f[3], f[4], false)
	if err != nil {
		return err
	}
	lon, err := nmeaLatLon(f[5], f[6], true)
	if err != nil {
		return err
	}
	pos := &Position{Latitude: lat, Longitude: lon}
	if spd, err := strconv.ParseFloat(f[7], 64); err == nil {
		pos.Speed = spd // knots
	}
	if crs, err := strconv.ParseFloat(f[8], 64); err == nil {
		c := int(crs + 0.5)
		if c > 0 && c <= 360 {
			pos.HasCourse = true
			pos.Course = c
		}
	}
	pkt.Position = pos
	pkt.Type = PacketPosition
	return nil
}

// nmeaLatLon parses a DDMM.mmmm (or DDDMM.mmmm) magnitude + hemisphere
// pair into signed decimal degrees. isLon selects 3-digit degrees.
func nmeaLatLon(mag, hemi string, isLon bool) (float64, error) {
	if len(mag) < 4 {
		return 0, errors.New("aprs: nmea lat/lon short")
	}
	dot := strings.IndexByte(mag, '.')
	if dot < 3 {
		return 0, errors.New("aprs: nmea lat/lon no minutes")
	}
	// Degrees = everything before the last two digits of the whole-part.
	degEnd := dot - 2
	deg, err := strconv.Atoi(mag[:degEnd])
	if err != nil {
		return 0, err
	}
	min, err := strconv.ParseFloat(mag[degEnd:], 64)
	if err != nil {
		return 0, err
	}
	v := float64(deg) + min/60.0
	switch hemi {
	case "N", "E":
		// positive
	case "S", "W":
		v = -v
	default:
		return 0, errors.New("aprs: nmea bad hemisphere")
	}
	_ = isLon
	return v, nil
}
