package app

import "context"

// wireServices constructs every owned component and appends it to
// a.startOrder as a namedComponent. Start iterates that slice in order.
//
// This is the commit-4 stub: it exists so that Run has something to
// call and the lifecycle machinery is complete even before the heavy
// wiring lands in commit 5. Any real App built via New + Run in the
// current commit will bring up zero components and immediately block
// on ctx.Done(), which is useful for lifecycle smoke tests and
// nothing else.
//
// Commit 5 replaces this body with the full wiring: configstore,
// modembridge, TX governor, KISS, AGW, digipeater, GPS, beacon,
// iGate, HTTP.
func (a *App) wireServices(ctx context.Context) error {
	_ = ctx
	return nil
}
