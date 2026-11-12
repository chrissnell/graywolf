package davis

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"

	"github.com/chrissnell/graywolf/pkg/weather"
)

func testPacket(kind PacketType) []byte {
	p := make([]byte, PacketLength)
	copy(p, "LOO")
	p[offType] = byte(kind)
	binary.LittleEndian.PutUint16(p[offBarometer:], 29921)
	binary.LittleEndian.PutUint16(p[offTempIn:], uint16(int16(700)))
	binary.LittleEndian.PutUint16(p[offTempOut:], uint16(int16(581)))
	p[offWind] = 7
	binary.LittleEndian.PutUint16(p[offWindDir:], 180)
	p[offHumidity] = 65
	binary.LittleEndian.PutUint16(p[offRainDay:], 37)
	if kind == PacketLOOP {
		binary.LittleEndian.PutUint16(p[offRainMonth:], 164)
	} else {
		binary.LittleEndian.PutUint16(p[offWindAvg10:], 75)
		binary.LittleEndian.PutUint16(p[offWindAvg2:], 80)
		binary.LittleEndian.PutUint16(p[offGust:], 155)
		binary.LittleEndian.PutUint16(p[offGustDir:], 270)
		binary.LittleEndian.PutUint16(p[offDewPoint:], 48)
		binary.LittleEndian.PutUint16(p[offRainHour:], 3)
		binary.LittleEndian.PutUint16(p[offRain24Hour:], 21)
	}
	crc := CRC(p[:PacketLength-2])
	p[PacketLength-2] = byte(crc >> 8)
	p[PacketLength-1] = byte(crc)
	return p
}

func TestDecodeAlternatingLOOPPackets(t *testing.T) {
	var got weather.Observation
	if typ, err := DecodeLOOP(testPacket(PacketLOOP), Bucket02MM, &got); err != nil || typ != PacketLOOP {
		t.Fatalf("LOOP decode type=%v err=%v", typ, err)
	}
	if typ, err := DecodeLOOP(testPacket(PacketLOOP2), Bucket02MM, &got); err != nil || typ != PacketLOOP2 {
		t.Fatalf("LOOP2 decode type=%v err=%v", typ, err)
	}
	assertNear(t, got.TemperatureC, 14.5)
	assertNear(t, got.HumidityPct, 65)
	assertNear(t, got.PressureHPa, 1013.2)
	assertNear(t, got.WindSpeedKPH, 11)
	assertNear(t, got.GustKPH, 24.9)
	assertNear(t, got.RainHourMM, 0.6)
	assertNear(t, got.Rain24hMM, 4.2)
	assertNear(t, got.RainDayMM, 7.4)
	assertNear(t, got.RainMonthMM, 32.8)
	if got.WindDirDeg == nil || *got.WindDirDeg != 180 || got.GustDirDeg == nil || *got.GustDirDeg != 270 {
		t.Fatalf("directions lost: %+v", got)
	}
}

func TestDecodeRejectsBadCRC(t *testing.T) {
	p := testPacket(PacketLOOP)
	p[20] ^= 0xff
	if _, err := DecodeLOOP(p, Bucket02MM, &weather.Observation{}); err == nil {
		t.Fatal("bad CRC accepted")
	}
}

func TestDecodeCapturedPacketFromDavisSimulator(t *testing.T) {
	const captured = "4c4f4fec00f5087274a4040f8a030a07" +
		"6801ffffffffffffffffffffffffffff" +
		"ff1cffffffffffffff00002aef020000" +
		"ffff00007901d1067e0034029206ffff" +
		"ffffffffff0000000000000000000000" +
		"00000000000000b503062df907bd030a" +
		"0d1298"
	packet, err := hex.DecodeString(captured)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) != PacketLength || CRC(packet) != 0 {
		t.Fatalf("captured packet length=%d CRC=%04x", len(packet), CRC(packet))
	}
	var got weather.Observation
	if typ, err := DecodeLOOP(packet, Bucket02MM, &got); err != nil || typ != PacketLOOP {
		t.Fatalf("captured LOOP type=%v err=%v", typ, err)
	}
	if got.TemperatureC == nil || got.PressureHPa == nil || got.HumidityPct == nil {
		t.Fatalf("captured observation incomplete: %+v", got)
	}
}

func assertNear(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 0.051 {
		t.Fatalf("value=%v want %.1f", got, want)
	}
}
