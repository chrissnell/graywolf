package diagcollect

import (
	"strings"
	"testing"
)

func TestParseOSReleasePrettyName(t *testing.T) {
	in := []byte(`PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
ID=debian
VERSION_ID="12"
`)
	got := parseOSReleasePretty(in)
	want := "Debian GNU/Linux 12 (bookworm)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseOSReleasePretty_Quoted(t *testing.T) {
	in := []byte(`PRETTY_NAME=Arch
`)
	if got := parseOSReleasePretty(in); got != "Arch" {
		t.Fatalf("got %q", got)
	}
}

func TestParseOSReleasePretty_Missing(t *testing.T) {
	if got := parseOSReleasePretty([]byte("ID=alpine\n")); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestExtractMACOUI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"b8:27:eb:11:22:33", "b8:27:eb"},
		{"B8-27-EB-11-22-33", "b8:27:eb"},
		{"", ""},
		{"too:short", ""},
	}
	for _, c := range cases {
		if got := extractMACOUI(c.in); got != c.want {
			t.Fatalf("extractMACOUI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCollectSystem_PopulatesOSAndArch(t *testing.T) {
	got := CollectSystem()
	if got.System.OS == "" {
		t.Fatal("OS empty")
	}
	if got.System.Arch == "" {
		t.Fatal("Arch empty")
	}
	if got.Hostname == "" {
		hasIssue := false
		for _, iss := range got.System.Issues {
			if strings.Contains(iss.Message, "hostname") {
				hasIssue = true
				break
			}
		}
		if !hasIssue {
			t.Fatal("hostname empty AND no issue recorded")
		}
	}
}

func TestCollectSystem_NetworkInterfacesHaveOUIOnly(t *testing.T) {
	got := CollectSystem()
	for _, ni := range got.System.NetworkInterfaces {
		if ni.MACOUI != "" && len(ni.MACOUI) != 8 {
			t.Fatalf("NIC %q has MACOUI %q (len %d), want 8 or empty", ni.Name, ni.MACOUI, len(ni.MACOUI))
		}
	}
}
