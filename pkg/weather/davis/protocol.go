// Package davis implements the Davis Vantage serial LOOP/LOOP2 protocol.
package davis

import (
	"encoding/binary"
	"fmt"

	"github.com/chrissnell/graywolf/pkg/weather"
)

const PacketLength = 99

type Bucket string

const (
	Bucket02MM  Bucket = "0.2mm"
	Bucket001In Bucket = "0.01in"
)

type PacketType byte

const (
	PacketLOOP  PacketType = 0
	PacketLOOP2 PacketType = 1
)

const (
	offTrend      = 3
	offType       = 4
	offBarometer  = 7
	offTempIn     = 9
	offTempOut    = 12
	offWind       = 14
	offWindDir    = 16
	offHumidity   = 33
	offRainRate   = 41
	offRainDay    = 50
	offRainMonth  = 52
	offWindAvg10  = 18
	offWindAvg2   = 20
	offGust       = 22
	offGustDir    = 24
	offDewPoint   = 30
	offRainHour   = 54
	offRain24Hour = 58
)

// CRC computes the Davis CRC-CCITT value (polynomial 0x1021, initial value
// zero). A complete valid 99-byte LOOP packet, including its CRC, returns zero.
func CRC(buf []byte) uint16 {
	var crc uint16
	for _, value := range buf {
		crc ^= uint16(value) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// DecodeLOOP validates one packet and merges the measurements carried by it
// into out. LOOP and LOOP2 deliberately update disjoint extension fields so
// two alternating packets progressively form one complete Observation.
func DecodeLOOP(packet []byte, bucket Bucket, out *weather.Observation) (PacketType, error) {
	if len(packet) != PacketLength {
		return 0, fmt.Errorf("davis: LOOP packet length %d, want %d", len(packet), PacketLength)
	}
	if string(packet[:3]) != "LOO" {
		return 0, fmt.Errorf("davis: invalid LOOP signature")
	}
	typ := PacketType(packet[offType])
	if typ != PacketLOOP && typ != PacketLOOP2 {
		return 0, fmt.Errorf("davis: unsupported LOOP packet type %d", typ)
	}
	if CRC(packet) != 0 {
		return 0, fmt.Errorf("davis: LOOP CRC mismatch")
	}
	if bucket != Bucket02MM && bucket != Bucket001In {
		return 0, fmt.Errorf("davis: unsupported rain bucket %q", bucket)
	}

	tempIn := int16(le16(packet, offTempIn))
	if tempIn != 32767 {
		out.TemperatureIndoorC = floatPtr(float64(f10ToC10(tempIn)) / 10)
	} else {
		out.TemperatureIndoorC = nil
	}
	tempOut := int16(le16(packet, offTempOut))
	if tempOut != 32767 {
		out.TemperatureC = floatPtr(float64(f10ToC10(tempOut)) / 10)
	} else {
		out.TemperatureC = nil
	}
	if packet[offHumidity] != 255 {
		out.HumidityPct = floatPtr(float64(packet[offHumidity]))
	} else {
		out.HumidityPct = nil
	}
	barometer := le16(packet, offBarometer)
	if barometer >= 20000 && barometer <= 32500 {
		hpa10 := (uint32(barometer)*33864 + 50000) / 100000
		out.PressureHPa = floatPtr(float64(hpa10) / 10)
	} else {
		out.PressureHPa = nil
	}
	windKPH := (uint16(packet[offWind])*1609 + 500) / 1000
	out.WindSpeedKPH = floatPtr(float64(windKPH))
	setDirection(&out.WindDirDeg, le16(packet, offWindDir))
	out.RainDayMM = floatPtr(float64(clicksToMM10(le16(packet, offRainDay), bucket)) / 10)
	raining := le16(packet, offRainRate) != 0
	out.Raining = &raining

	if typ == PacketLOOP {
		out.RainMonthMM = floatPtr(float64(clicksToMM10(le16(packet, offRainMonth), bucket)) / 10)
		return typ, nil
	}
	out.WindAvg10KPH = floatPtr(float64(mph10ToKPH10(le16(packet, offWindAvg10))) / 10)
	out.WindAvg2KPH = floatPtr(float64(mph10ToKPH10(le16(packet, offWindAvg2))) / 10)
	out.GustKPH = floatPtr(float64(mph10ToKPH10(le16(packet, offGust))) / 10)
	setDirection(&out.GustDirDeg, le16(packet, offGustDir))
	dew := int16(le16(packet, offDewPoint))
	if dew != 255 {
		out.DewPointC = floatPtr(float64(f10ToC10(dew*10)) / 10)
	} else {
		out.DewPointC = nil
	}
	out.RainHourMM = floatPtr(float64(clicksToMM10(le16(packet, offRainHour), bucket)) / 10)
	out.Rain24hMM = floatPtr(float64(clicksToMM10(le16(packet, offRain24Hour), bucket)) / 10)
	return typ, nil
}

func le16(packet []byte, offset int) uint16 { return binary.LittleEndian.Uint16(packet[offset:]) }

func f10ToC10(f10 int16) int16 {
	t := (int32(f10) - 320) * 5
	if t < 0 {
		return int16((t - 4) / 9)
	}
	return int16((t + 4) / 9)
}

func mph10ToKPH10(mph10 uint16) uint16 {
	return uint16((uint32(mph10)*1609 + 500) / 1000)
}

func clicksToMM10(clicks uint16, bucket Bucket) uint16 {
	if bucket == Bucket02MM {
		return clicks * 2
	}
	return uint16((uint32(clicks)*254 + 50) / 100)
}

func setDirection(target **int, raw uint16) {
	if raw < 1 || raw > 360 {
		*target = nil
		return
	}
	direction := int(raw)
	if direction == 360 {
		direction = 0
	}
	*target = &direction
}

func floatPtr(value float64) *float64 { return &value }
