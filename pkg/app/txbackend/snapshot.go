package txbackend

// BuildSnapshot builds a fresh Snapshot from the given raw
// inputs. Pure function — no I/O, no locking — so the caller (the
// watcher goroutine) can invoke it off the hot path and Publish the
// result with a single atomic store.
//
// Inputs:
//
//   - modem: the shared ModemBackend (may be nil if modembridge isn't
//     wired in tests; the snapshot will then have zero modem backends).
//   - modemChannels: the set of channel IDs with a bound input audio
//     device. Used to populate the modem backend's ByChannel entries
//     even though ModemBackend exposes AttachedChannels itself — we
//     want the caller to be explicit so test wiring can exercise
//     partial configurations without stubbing a full modem backend.
//   - kissBackends: one *KissTncBackend per eligible KissInterface row.
//     Membership is decided by the caller per D4: Mode=tnc AND
//     AllowTxFromGovernor=true.
//
// CSMA skip is computed per channel: true iff the channel has zero
// modem backends (no carrier to sense).
func BuildSnapshot(modem *ModemBackend, modemChannels []uint32, kissBackends []*KissTncBackend) *Snapshot {
	snap := newSnapshot()

	// Track which channels have a modem attachment so the kiss pass
	// can toggle CsmaSkip correctly.
	hasModem := make(map[uint32]bool, len(modemChannels))
	if modem != nil {
		for _, ch := range modemChannels {
			hasModem[ch] = true
			snap.ByChannel[ch] = append(snap.ByChannel[ch], modem)
		}
	}

	for _, kb := range kissBackends {
		if kb == nil {
			continue
		}
		for _, ch := range kb.AttachedChannels() {
			snap.ByChannel[ch] = append(snap.ByChannel[ch], kb)
		}
	}

	// Compute CSMA skip: true for every channel that has at least one
	// kiss-tnc backend and zero modem backends.
	for ch, backends := range snap.ByChannel {
		if hasModem[ch] {
			continue
		}
		// Channel has backends but none are modem → KISS-only → skip CSMA.
		if len(backends) > 0 {
			snap.CsmaSkip[ch] = true
		}
	}
	return snap
}
