//go:build !android

package app

// Desktop App carries no platformsvc client. This empty struct keeps
// the embed point in App's definition build-clean across both targets.
type appAndroidExt struct{}
