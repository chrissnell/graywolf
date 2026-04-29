package diagcollect

import (
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// SystemResult bundles the System section with the host's literal
// hostname so the orchestrator can prime the redact engine before the
// final scrub pass. The hostname field is the only string that leaves
// this collector unscrubbed; everything inside Result.System is the
// caller's to feed into the engine.
type SystemResult struct {
	System   flareschema.System
	Hostname string
}

// CollectSystem returns the OS+hardware+identity snapshot. Per-OS
// fields are populated by osIdentity (build-tag specialized);
// cross-platform fields land here.
func CollectSystem() SystemResult {
	var res SystemResult
	res.System.OS = runtime.GOOS
	res.System.Arch = runtime.GOARCH

	if name, err := os.Hostname(); err == nil && name != "" {
		res.Hostname = name
	} else if err != nil {
		res.System.Issues = append(res.System.Issues, flareschema.CollectorIssue{
			Kind:    "hostname_unavailable",
			Message: "os.Hostname: " + err.Error(),
		})
	}

	identity, idIssues := osIdentity()
	res.System.OSPretty = identity.OSPretty
	res.System.Kernel = identity.Kernel
	res.System.IsRaspberryPi = identity.IsRaspberryPi
	res.System.PiModel = identity.PiModel
	res.System.NTPSynchronized = identity.NTPSynchronized
	res.System.UdevRulesPresent = identity.UdevRulesPresent
	res.System.Groups = identity.Groups
	res.System.Issues = append(res.System.Issues, idIssues...)

	res.System.NetworkInterfaces = collectNetworkInterfaces(&res.System)

	return res
}

// osIdentityResult is what each per-OS file returns. Populated fields
// vary; unpopulated ones stay zero.
type osIdentityResult struct {
	OSPretty         string
	Kernel           string
	IsRaspberryPi    bool
	PiModel          string
	NTPSynchronized  bool
	UdevRulesPresent []string
	Groups           []string
}

// parseOSReleasePretty extracts PRETTY_NAME from /etc/os-release
// content. Quoted and unquoted forms are both supported. Empty
// return when the field is absent.
func parseOSReleasePretty(content []byte) string {
	lines := strings.Split(string(content), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "PRETTY_NAME=") {
			continue
		}
		val := strings.TrimPrefix(l, "PRETTY_NAME=")
		val = strings.Trim(val, `"`)
		return val
	}
	return ""
}

// extractMACOUI returns the lowercase OUI ("xx:xx:xx") of a MAC
// address. Hyphenated forms are converted to colon form first. Returns
// "" for empty input or any string that doesn't have colons at the
// expected OUI positions (indices 2 and 5).
func extractMACOUI(mac string) string {
	if mac == "" {
		return ""
	}
	norm := strings.ReplaceAll(strings.ToLower(mac), "-", ":")
	if len(norm) < 8 || norm[2] != ':' || norm[5] != ':' {
		return ""
	}
	return norm[:8]
}

// collectNetworkInterfaces walks net.Interfaces() and emits one
// flareschema.NetworkInterface per interface with the MAC reduced to
// OUI-only and addresses recorded as CIDR strings. Loopback addresses
// (127.0.0.1, ::1) are filtered out — they're never useful for
// diagnosing radio-host connectivity. Per-interface address-lookup
// failures degrade gracefully (the interface still appears, just
// without addresses) rather than dropping the row or returning early.
func collectNetworkInterfaces(sys *flareschema.System) []flareschema.NetworkInterface {
	out := make([]flareschema.NetworkInterface, 0)
	ifaces, err := net.Interfaces()
	if err != nil {
		sys.Issues = append(sys.Issues, flareschema.CollectorIssue{
			Kind:    "network_interfaces_unavailable",
			Message: err.Error(),
		})
		return out
	}
	for _, iface := range ifaces {
		ipv4, ipv6 := interfaceAddrs(&iface, sys)
		out = append(out, flareschema.NetworkInterface{
			Name:   iface.Name,
			MACOUI: extractMACOUI(iface.HardwareAddr.String()),
			Up:     iface.Flags&net.FlagUp != 0,
			IPv4:   ipv4,
			IPv6:   ipv6,
			MTU:    iface.MTU,
		})
	}
	return out
}

// interfaceAddrs splits one interface's addresses into IPv4 and IPv6
// CIDR-string slices, filtering out loopback. Errors are recorded as a
// single issue keyed by interface name; the function still returns
// whatever it managed to collect.
func interfaceAddrs(iface *net.Interface, sys *flareschema.System) (ipv4, ipv6 []string) {
	addrs, err := iface.Addrs()
	if err != nil {
		sys.Issues = append(sys.Issues, flareschema.CollectorIssue{
			Kind:    "network_interface_addrs_unavailable",
			Message: iface.Name + ": " + err.Error(),
		})
		return nil, nil
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet == nil || ipNet.IP.IsLoopback() {
			continue
		}
		s := ipNet.String()
		if ipNet.IP.To4() != nil {
			ipv4 = append(ipv4, s)
		} else {
			ipv6 = append(ipv6, s)
		}
	}
	return ipv4, ipv6
}
