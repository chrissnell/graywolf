package beacon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	weatherobs "github.com/chrissnell/graywolf/pkg/weather"
)

func writeWxNowTestFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "WxNow.txt")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadWxNow(t *testing.T) {
	path := writeWxNowTestFile(t, "Feb 01 2009 12:34\r\n272/010g006t069r010p030P020h61b10150\r\n")
	observation, err := weatherobs.ReadWxNow(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := weatherobs.EncodeAPRS(observation)
	if err != nil {
		t.Fatal(err)
	}
	if want := "272/010g006t069r010p030P020h61b10150"; got != want {
		t.Fatalf("ReadWxNow() = %q, want %q", got, want)
	}
}

func TestReadWxNowRejectsMalformedFiles(t *testing.T) {
	for _, tc := range []struct {
		name, contents, want string
	}{
		{"missing weather line", "Feb 01 2009 12:34\n", "must contain"},
		{"bad timestamp", "not a timestamp\n272/010g006t069\n", "timestamp"},
		{"not weather", "Feb 01 2009 12:34\nhello world\n", "not a complete"},
		{"weather line too long", "Feb 01 2009 12:34\nt069" + strings.Repeat("x", 237) + "\n", "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := weatherobs.ReadWxNow(writeWxNowTestFile(t, tc.contents))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ReadWxNow() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
