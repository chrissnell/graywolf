package platformproto

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestPttMethodEnumSync_AcrossProtoRustKotlin asserts that the PttMethod enum
// int→name mapping is identical across all three sources: proto (Go), Rust, and Kotlin.
// This test directly addresses Appendix B's drift-hazard call-out by parsing each source
// file and comparing the resulting mappings.
func TestPttMethodEnumSync_AcrossProtoRustKotlin(t *testing.T) {
	// 1. Extract proto mapping from Go generated code.
	protoMap := make(map[string]int32)
	for v, name := range PttMethod_name {
		protoMap[name] = v
	}

	// Resolve paths relative to the workspace root.
	// Start from the pkg/platformproto directory and work up to the repo root.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}

	// Try to find the repo root by looking for go.mod.
	repoRoot := findRepoRoot(cwd)
	if repoRoot == "" {
		t.Fatalf("could not find repository root (no go.mod found)")
	}

	// 2. Parse Rust constants from graywolf-modem/src/tx/ptt_android_consts.rs
	rustPath := filepath.Join(repoRoot, "graywolf-modem", "src", "tx", "ptt_android_consts.rs")
	rustMap, err := parseRustConstants(rustPath)
	if err != nil {
		t.Fatalf("failed to parse Rust constants: %v", err)
	}

	// 3. Parse Kotlin constants from android/app/src/main/kotlin/com/nw5w/graywolf/usb/PttMethodConsts.kt
	kotlinPath := filepath.Join(repoRoot, "android", "app", "src", "main", "kotlin", "com", "nw5w", "graywolf", "usb", "PttMethodConsts.kt")
	kotlinMap, err := parseKotlinConstants(kotlinPath)
	if err != nil {
		t.Fatalf("failed to parse Kotlin constants: %v", err)
	}

	// 4. Compare all three mappings.
	if !mapsEqual(protoMap, rustMap) {
		t.Fatalf("proto and Rust mappings differ:\nproto: %v\nrust: %v", protoMap, rustMap)
	}
	if !mapsEqual(protoMap, kotlinMap) {
		t.Fatalf("proto and Kotlin mappings differ:\nproto: %v\nkotlin: %v", protoMap, kotlinMap)
	}
	if !mapsEqual(rustMap, kotlinMap) {
		t.Fatalf("Rust and Kotlin mappings differ:\nrust: %v\nkotlin: %v", rustMap, kotlinMap)
	}
}

// parseRustConstants extracts PTT_METHOD_* constants from the Rust source file
// using a regex that matches: pub const PTT_METHOD_NAME: i32 = VALUE;
func parseRustConstants(filePath string) (map[string]int32, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Rust file: %w", err)
	}

	// Pattern: pub const PTT_METHOD_NAME: i32 = VALUE;
	pattern := regexp.MustCompile(`pub const (PTT_METHOD_\w+):\s*i32\s*=\s*(\d+);`)
	matches := pattern.FindAllStringSubmatch(string(content), -1)

	if len(matches) == 0 {
		return nil, fmt.Errorf("no Rust constants found in %s", filePath)
	}

	result := make(map[string]int32)
	for _, match := range matches {
		name := match[1]
		var value int32
		fmt.Sscanf(match[2], "%d", &value)
		result[name] = value
	}

	if len(result) < 5 {
		return nil, fmt.Errorf("expected at least 5 constants in Rust file, found %d", len(result))
	}

	return result, nil
}

// parseKotlinConstants extracts PTT_METHOD_* constants from the Kotlin source file
// using a regex that matches: const val PTT_METHOD_NAME = VALUE
func parseKotlinConstants(filePath string) (map[string]int32, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Kotlin file: %w", err)
	}

	// Pattern: const val PTT_METHOD_NAME = VALUE (with optional spaces)
	pattern := regexp.MustCompile(`const val (PTT_METHOD_\w+)\s*=\s*(\d+)`)
	matches := pattern.FindAllStringSubmatch(string(content), -1)

	if len(matches) == 0 {
		return nil, fmt.Errorf("no Kotlin constants found in %s", filePath)
	}

	result := make(map[string]int32)
	for _, match := range matches {
		name := match[1]
		var value int32
		fmt.Sscanf(match[2], "%d", &value)
		result[name] = value
	}

	if len(result) < 5 {
		return nil, fmt.Errorf("expected at least 5 constants in Kotlin file, found %d", len(result))
	}

	return result, nil
}

// mapsEqual compares two maps of string→int32, accounting for proto-namespaced keys.
// Proto keys are "PTT_METHOD_UNKNOWN", "PTT_METHOD_CP2102N_RTS", etc.
// Rust and Kotlin keys are the same format.
func mapsEqual(m1, m2 map[string]int32) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k, v := range m1 {
		if v2, exists := m2[k]; !exists || v != v2 {
			return false
		}
	}
	return true
}

// findRepoRoot walks up from the current working directory until it finds go.mod,
// which marks the repository root. Returns empty string if not found.
func findRepoRoot(startPath string) string {
	current := startPath
	for {
		gomodPath := filepath.Join(current, "go.mod")
		if _, err := os.Stat(gomodPath); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root without finding go.mod
			break
		}
		current = parent
	}
	return ""
}
