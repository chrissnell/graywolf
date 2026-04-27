package diagcollect

import (
	"encoding/json"
	"fmt"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// CollectUSB invokes graywolf-modem --list-usb via the default runner.
func CollectUSB(bin string) flareschema.USBTopology {
	return collectUSBWith(defaultRunner{}, bin)
}

func collectUSBWith(r Runner, bin string) flareschema.USBTopology {
	out := flareschema.USBTopology{
		Devices: make([]flareschema.USBDevice, 0),
	}
	raw, issue := r.Run(bin, "--list-usb")
	if issue != nil {
		out.Issues = append(out.Issues, *issue)
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind:    "usb_decode_failed",
			Message: fmt.Sprintf("%v: stdout=%q", err, truncateForIssue(string(raw))),
		})
	}
	return out
}
