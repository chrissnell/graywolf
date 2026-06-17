//go:build android

package clocksync

// Check is a no-op on Android. Android is Linux, but its seccomp-bpf
// policy forbids adjtimex(2) for app processes: invoking it delivers
// SIGSYS and kills the process, even for the read-only zero-Modes query
// that needs no privileges elsewhere (graywolf#315). With no usable
// query API we report Unknown and the caller stays silent, matching the
// non-Linux behavior in clocksync_other.go.
func Check() Status { return Unknown }
