package diagcollect

import (
	"encoding/json"
	"fmt"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// CollectCM108 invokes graywolf-modem --list-cm108 and wraps the
// flat-array stdout into a section.
func CollectCM108(bin string) flareschema.CM108Devices {
	return collectCM108With(defaultRunner{}, bin)
}

func collectCM108With(r Runner, bin string) flareschema.CM108Devices {
	out := flareschema.CM108Devices{
		Devices: make([]flareschema.CM108Device, 0),
	}
	raw, issue := r.Run(bin, "--list-cm108")
	if issue != nil {
		out.Issues = append(out.Issues, *issue)
		return out
	}
	var arr []flareschema.CM108Device
	if err := json.Unmarshal(raw, &arr); err != nil {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind:    "cm108_decode_failed",
			Message: fmt.Sprintf("%v: stdout=%q", err, truncateForIssue(string(raw))),
		})
		return out
	}
	out.Devices = arr
	return out
}
