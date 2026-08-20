//go:build !android

package app

// injectAndroidBLEClient is a no-op on non-Android builds.
func (a *App) injectAndroidBLEClient() {}
