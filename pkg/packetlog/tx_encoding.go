package packetlog

import "time"

type txKey struct {
	id      uint64
	channel uint32
}
type pendingEncoding struct {
	encoding *TXEncoding
	at       time.Time
}

// SetTXEncoding attaches a native encoder report to its original TX row.
// The report and governor hook may arrive in either order. Unmatched reports
// are bounded and expire; frame ID + channel prevents confusing later repeats.
// Existing rows are enriched in place for API polling, never recorded again or
// rebroadcast as another packet to subscribers.
func (l *Log) SetTXEncoding(id uint64, channel uint32, encoding TXEncoding) {
	if id == 0 || (encoding.Encoding != "fx25" && encoding.Encoding != "ax25") {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := 0; i < l.size; i++ {
		idx := (l.head - 1 - i + l.cap) % l.cap
		e := &l.entries[idx]
		if e.Direction == DirTX && e.FrameID == id && e.Channel == channel {
			e.TXEncoding = &encoding
			return
		}
	}
	now := time.Now()
	for k, v := range l.pendingEncoding {
		if now.Sub(v.at) >= time.Minute {
			delete(l.pendingEncoding, k)
		}
	}
	if l.pendingEncoding == nil {
		l.pendingEncoding = make(map[txKey]pendingEncoding)
	}
	if len(l.pendingEncoding) >= 128 {
		var oldest txKey
		var at time.Time
		for k, v := range l.pendingEncoding {
			if at.IsZero() || v.at.Before(at) {
				oldest, at = k, v.at
			}
		}
		delete(l.pendingEncoding, oldest)
	}
	l.pendingEncoding[txKey{id, channel}] = pendingEncoding{&encoding, now}
}
