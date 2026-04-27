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

func TestParseMountinfoPathComponentPrefix(t *testing.T) {
	// One line per mount, fabricated to mirror real /proc/self/mountinfo
	// shape: id parent maj:min root mountpoint opts - fstype source super
	const content = `1 0 8:1 / / rw,relatime - ext4 /dev/sda1 rw
36 1 0:31 / /var/lib/foo rw - ext4 /dev/sda3 rw
40 1 0:32 / /var/lib/foobar rw - ext4 /dev/mmcblk0p2 rw
`
	cases := []struct {
		path string
		want string
	}{
		// Exact match on /var/lib/foobar must NOT be misattributed to
		// the sibling mount /var/lib/foo.
		{"/var/lib/foobar/graywolf-logs.db", "/dev/mmcblk0p2"},
		{"/var/lib/foo/graywolf-logs.db", "/dev/sda3"},
		// A path that exists under no specific mount falls through to root.
		{"/etc/hostname", "/dev/sda1"},
	}
	for _, c := range cases {
		got, err := parseMountinfo([]byte(content), c.path)
		if err != nil {
			t.Fatalf("parseMountinfo(%q): %v", c.path, err)
		}
		if got != c.want {
			t.Errorf("parseMountinfo(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestParseMountinfoNoMatch(t *testing.T) {
	// Content with no entries at all returns an error.
	if _, err := parseMountinfo([]byte(""), "/var"); err == nil {
		t.Fatal("expected error for empty mountinfo")
	}
}
