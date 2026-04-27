package aprs

import (
	"errors"
	"strings"
)

// parseObject handles ';NNNNNNNNN*DDHHMMzLLLL.LLN/LLLLL.LLW$comment' and
// the compressed form. Name is exactly 9 bytes; status byte '*' == live,
// '_' == killed; 7-byte timestamp follows; then position identical to
// '!'/'=' bodies.
func parseObject(pkt *DecodedAPRSPacket, info []byte) error {
	if len(info) < 1+9+1+7 {
		return errors.New("aprs: object too short")
	}
	name := strings.TrimRight(string(info[1:10]), " ")
	live := info[10] == '*'
	if !live && info[10] != '_' {
		return errors.New("aprs: object status byte")
	}
	ts, err := parseAPRSTimestamp(info[11:18])
	if err != nil {
		return err
	}
	obj := &Object{Name: name, Live: live, Timestamp: ts}
	inner := &DecodedAPRSPacket{Type: PacketPosition}
	if err := parsePositionBody(inner, info[18:]); err != nil {
		return err
	}
	obj.Position = inner.Position
	obj.Comment = inner.Comment
	if inner.Weather != nil {
		pkt.Weather = inner.Weather
	}
	if inner.DF != nil {
		pkt.DF = inner.DF
	}
	pkt.Object = obj
	pkt.Type = PacketObject
	return nil
}

// parseItem handles ')NNNNN!LLLL.LLN/LLLLL.LLW$comment'. Name is 3..9
// bytes terminated by '!' (live) or '_' (killed).
func parseItem(pkt *DecodedAPRSPacket, info []byte) error {
	if len(info) < 5 {
		return errors.New("aprs: item too short")
	}
	// APRS101 ch 14: item name is 3..9 chars, terminator at positions
	// 4..10 of the info field ( info[0]=')' + 3..9 name bytes ).
	var termIdx = -1
	for i := 4; i <= 10 && i < len(info); i++ {
		if info[i] == '!' || info[i] == '_' {
			termIdx = i
			break
		}
	}
	if termIdx < 4 {
		return errors.New("aprs: item terminator missing")
	}
	name := string(info[1:termIdx])
	live := info[termIdx] == '!'
	body := info[termIdx+1:]
	item := &Item{Name: name, Live: live}
	inner := &DecodedAPRSPacket{Type: PacketPosition}
	if err := parsePositionBody(inner, body); err != nil {
		return err
	}
	item.Position = inner.Position
	item.Comment = inner.Comment
	if inner.Weather != nil {
		pkt.Weather = inner.Weather
	}
	if inner.DF != nil {
		pkt.DF = inner.DF
	}
	pkt.Item = item
	pkt.Type = PacketItem
	return nil
}
