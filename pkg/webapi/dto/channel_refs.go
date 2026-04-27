package dto

import (
	"context"
	"fmt"
)

// ChannelLookup is the narrow read surface DTO validators need to reject
// writes that reference a non-existent channel. Implemented by
// *configstore.Store via its ChannelExists method. Kept as an interface
// so tests can stub it without pulling the full configstore into scope,
// and so the DTO package doesn't need to import configstore just to
// typecheck the signature (DTOs already depend on configstore for model
// types; the interface is a convenience, not an isolation layer).
type ChannelLookup interface {
	ChannelExists(ctx context.Context, id uint32) (bool, error)
}

// ValidateChannelRef rejects a write whose channelID points at a
// non-existent channel. The zero value is treated as "none" — several
// soft-FK columns (IGateConfig.TxChannel, a TxTiming row's channel
// after a cascade nulls it, etc.) use 0 as the sentinel for "unset" and
// must pass through this helper unchanged. A non-zero value that fails
// to resolve returns a human-legible error intended to land verbatim in
// a 400 response body.
//
// Callers wire this into the request lifecycle AFTER the DTO's own
// Validate() method has passed — keeping Validate() pure (no I/O) and
// letting the handler thread the store into the cross-table check. This
// mirrors the pattern Phase 3 established for the mutual-exclusivity
// rule, which lives at the configstore layer and runs once per
// create/update regardless of which DTO triggered it.
//
// Note: this helper does not hold a DB lock across the lookup and the
// subsequent store write, so a racing delete between the check and the
// write could still land an orphan. The store-layer ChannelReferrers
// + post-delete reload notify path catches that class of race at the
// next reload; DTO validation is a friendly-error gate, not a hard
// invariant.
func ValidateChannelRef(ctx context.Context, lookup ChannelLookup, fieldName string, channelID uint32) error {
	if channelID == 0 {
		return nil
	}
	if lookup == nil {
		// Defensive: if a caller forgot to thread the store, reject
		// rather than silently accepting. The symptom will be obvious
		// in unit tests.
		return fmt.Errorf("%s: channel lookup unavailable", fieldName)
	}
	exists, err := lookup.ChannelExists(ctx, channelID)
	if err != nil {
		return fmt.Errorf("%s: look up channel %d: %w", fieldName, channelID, err)
	}
	if !exists {
		return fmt.Errorf("%s: channel %d does not exist", fieldName, channelID)
	}
	return nil
}
