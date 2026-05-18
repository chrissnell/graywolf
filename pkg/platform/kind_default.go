//go:build !android

// Package platform exposes a single source-of-truth Kind constant for
// the host platform. Used by SPA / Go / Kotlin sites that need to gate
// behavior cross-compile-safely. The companion JS module is
// web/src/lib/platform.js; the Kotlin one is android/.../Platform.kt.
package platform

// Kind is the canonical platform identifier on this build.
// Desktop (Linux/macOS/Windows) and the Android phone build are the
// only two values today; future platforms add more build-tagged files.
const Kind = "desktop"
