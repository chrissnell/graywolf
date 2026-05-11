package platform_test

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/platform"
)

func TestKindNotEmpty(t *testing.T) {
	if platform.Kind == "" {
		t.Fatal("platform.Kind is empty; expected one of: android, desktop")
	}
}

func TestKindKnownValue(t *testing.T) {
	switch platform.Kind {
	case "android", "desktop":
		// ok
	default:
		t.Fatalf("platform.Kind=%q is not one of the known values", platform.Kind)
	}
}
