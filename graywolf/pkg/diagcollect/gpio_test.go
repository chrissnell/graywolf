package diagcollect

import "testing"

func TestParseSysfsLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"pinctrl-bcm2711\n", "pinctrl-bcm2711"},
		{"chip foo\n", "chip foo"},
		{"", ""},
	}
	for _, c := range cases {
		if got := parseSysfsLabel(c.in); got != c.want {
			t.Fatalf("parseSysfsLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseSysfsNGPIO(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"58\n", 58},
		{"0\n", 0},
		{"junk", 0},
	}
	for _, c := range cases {
		if got := parseSysfsNGPIO(c.in); got != c.want {
			t.Fatalf("parseSysfsNGPIO(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
