package platformsvc

// SchemaVersion is the wire-schema version negotiated by the platform
// service Hello handshake. Kept in a non-build-tagged file so the Go
// drift-guard test (schema_version_test.go) can read it on any host
// without requiring GOOS=android. The Kotlin side must declare the
// same value at GraywolfService.kt:schemaVersion — schema_version_test
// asserts this.
const SchemaVersion uint32 = 3
