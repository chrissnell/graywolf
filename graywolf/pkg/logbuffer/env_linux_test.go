//go:build linux

package logbuffer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsRaspberryPiModel(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"pi4", "Raspberry Pi 4 Model B Rev 1.4\x00", true},
		{"pi5", "Raspberry Pi 5 Model B Rev 1.0\x00", true},
		{"generic", "Generic ARM board\x00", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "model")
			if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := isRaspberryPi(path); got != c.want {
				t.Fatalf("isRaspberryPi(%q) = %v, want %v", c.content, got, c.want)
			}
		})
	}
}

func TestIsRaspberryPiMissingFile(t *testing.T) {
	if isRaspberryPi(filepath.Join(t.TempDir(), "absent")) {
		t.Fatal("missing file must report false")
	}
}

func TestIsSDCardDevice(t *testing.T) {
	cases := map[string]bool{
		"/dev/mmcblk0p2": true,
		"/dev/mmcblk1":   true,
		"/dev/mtdblock3": true,
		"/dev/sda1":      false,
		"/dev/nvme0n1p1": false,
		"/dev/root":      false,
		"":               false,
	}
	for in, want := range cases {
		if got := isSDCardDevice(in); got != want {
			t.Errorf("isSDCardDevice(%q) = %v, want %v", in, got, want)
		}
	}
}
