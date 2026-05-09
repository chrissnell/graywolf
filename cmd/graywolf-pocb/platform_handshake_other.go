//go:build !android

package main

// dialPlatformAndHello is a desktop-build no-op so cmd/graywolf-pocb stays
// buildable on the operator's bench. The android-tagged sibling does the
// real Hello round-trip.
func dialPlatformAndHello(_ string) error { return nil }
