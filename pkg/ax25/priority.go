package ax25

// TX governor priority levels. Higher value = sent sooner.
//
// These live in pkg/ax25 (a leaf package imported by every protocol
// package) so that submitters in pkg/kiss, pkg/agw, pkg/aprs, and future
// digipeater/iGate packages can reference the same values without
// importing pkg/txgovernor (which would create an import cycle).
//
// pkg/txgovernor re-defines the same constants for its own API surface
// and asserts equality at init time; if these ever drift, the test in
// pkg/txgovernor will fail.
const (
	PriorityBeacon     = 1 // scheduled beacons
	PriorityDigipeated = 2 // digipeater-repeated traffic
	PriorityClient     = 3 // KISS/AGW client-originated
	PriorityIGateMsg   = 4 // iGate-delivered directed message
)
