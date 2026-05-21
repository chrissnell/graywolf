//go:build !android

package app

import "github.com/chrissnell/graywolf/pkg/webapi"

// pttSourceForWebapi returns nil on desktop builds so the webapi
// /api/ptt/available handler falls back to its native pttdevice.Enumerate()
// path (serial ports + gpiochips + CM108 HID). Mirrors
// btSourceForWebapi() in btsource_default.go — the Android-only adapter
// lives in pttsource_android.go.
func (a *App) pttSourceForWebapi() webapi.PttDeviceSource { return nil }
