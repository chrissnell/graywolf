package com.nw5w.graywolf

/**
 * Single source-of-truth platform identifier for Kotlin sites.
 * Always "android" today (this module only ships in the Android APK).
 * Companion of pkg/platform.Kind (Go) and Platform.kind (web SPA).
 *
 * Provides a stable import surface so future iOS-shared Kotlin
 * Multiplatform code only needs to flip this one constant per target.
 */
object Platform {
    const val KIND = "android"
}
