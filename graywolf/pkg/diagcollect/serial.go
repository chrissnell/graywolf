package diagcollect

import (
	"github.com/chrissnell/graywolf/pkg/flareschema"
	"github.com/chrissnell/graywolf/pkg/pttdevice"
)

// CollectPTT enumerates PTT-capable devices via pttdevice.Enumerate
// and shapes the result into a flareschema.PTTSection.
//
// pttdevice already handles per-OS quirks (macOS /dev/cu.* preference,
// Linux sysfs USB lookup) — we don't duplicate any of that here.
func CollectPTT() flareschema.PTTSection {
	devs := pttdevice.Enumerate()
	return convertPTT(devs)
}

// convertPTT is the testable inner: takes the raw enumerator output
// and produces the section.
func convertPTT(devs []pttdevice.AvailableDevice) flareschema.PTTSection {
	out := flareschema.PTTSection{
		Candidates: make([]flareschema.PTTCandidate, 0, len(devs)),
	}
	for _, d := range devs {
		out.Candidates = append(out.Candidates, flareschema.PTTCandidate{
			Kind:        mapPttKind(d.Type),
			Path:        d.Path,
			Vendor:      d.USBVendor,
			Product:     d.USBProduct,
			Description: d.Description,
		})
	}
	return out
}

// mapPttKind translates pttdevice's Type values into the schema's
// Kind values. Unmapped values pass through unchanged.
func mapPttKind(t string) string {
	switch t {
	case "serial":
		return "serial"
	case "gpio":
		return "gpio_chip"
	case "cm108":
		return "cm108_hid"
	default:
		return t
	}
}
