package kiss

import (
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
)

// ChannelStat is a cumulative per-channel frame count for a TNC-mode
// KISS channel. RxFrames counts AX.25 frames ingested from the TNC
// (per inbound frame, per interface — genuinely distinct off-air
// traffic). TxFrames counts frames dispatched to the channel by the
// TX backend, incremented once per frame regardless of how many
// KISS-TNC interfaces the channel fans out to, so it stays in
// lockstep with the aggregate TX tile rather than multiplying by
// fan-out width; it is a dispatched count, not an on-air
// confirmation. Bad-FCS is deliberately absent: a hardware TNC
// validates the FCS itself and never forwards a bad frame over KISS,
// so the dashboard's "Bad FCS" stays 0 for these channels (issue
// #132).
type ChannelStat struct {
	RxFrames uint64
	TxFrames uint64
}

// countRx records one received frame on ch.
func (m *Manager) countRx(ch uint32) {
	m.chanStatsMu.Lock()
	s := m.chanStats[ch]
	if s == nil {
		s = &ChannelStat{}
		m.chanStats[ch] = s
	}
	s.RxFrames++
	m.chanStatsMu.Unlock()
}

// RecordChannelTx records one dispatched TX frame on ch. Called by
// the TX backend dispatcher (once per frame, fan-out-safe) rather
// than from the per-instance tx queue, so the count matches the
// aggregate TX tile on channels with multiple KISS-TNC interfaces
// (issue #132).
func (m *Manager) RecordChannelTx(ch uint32) { m.countTx(ch) }

// countTx records one transmitted frame on ch.
func (m *Manager) countTx(ch uint32) {
	m.chanStatsMu.Lock()
	s := m.chanStats[ch]
	if s == nil {
		s = &ChannelStat{}
		m.chanStats[ch] = s
	}
	s.TxFrames++
	m.chanStatsMu.Unlock()
}

// ChannelStats returns a snapshot of the cumulative RX/TX counts for a
// single channel. ok is false when no TNC-mode frame has yet been seen
// on that channel, letting callers (webapi) prefer modembridge's cache
// when the channel is modem-backed instead.
func (m *Manager) ChannelStats(ch uint32) (ChannelStat, bool) {
	m.chanStatsMu.Lock()
	defer m.chanStatsMu.Unlock()
	s, ok := m.chanStats[ch]
	if !ok {
		return ChannelStat{}, false
	}
	return *s, true
}

// wrapRxIngress decorates base so every TNC-mode frame it carries is
// counted against rf.Channel before delegating. base may be nil (no
// TNC routing configured); the wrapper is then a no-op counter that
// still tolerates being called. The server only invokes RxIngress in
// ModeTnc, so this counts exactly KISS-TNC RX with the precise
// resolved channel.
func (m *Manager) wrapRxIngress(base func(rf *pb.ReceivedFrame, src ingress.Source)) func(rf *pb.ReceivedFrame, src ingress.Source) {
	return func(rf *pb.ReceivedFrame, src ingress.Source) {
		if rf != nil {
			m.countRx(rf.Channel)
		}
		if base != nil {
			base(rf, src)
		}
	}
}
