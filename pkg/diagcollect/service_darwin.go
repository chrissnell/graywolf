//go:build darwin

package diagcollect

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// CollectServiceStatus on macOS probes launchd via `launchctl print
// system/com.chrissnell.graywolf`. If the label is unknown, that's
// the typical "graywolf is being run interactively" case — emit a
// not_supported issue rather than a hard failure.
func CollectServiceStatus() flareschema.ServiceStatus {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return notSupportedServiceStatus("launchctl not on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "launchctl", "print", "system/com.chrissnell.graywolf").CombinedOutput()
	if err != nil {
		return flareschema.ServiceStatus{
			Manager: "launchd",
			Unit:    "com.chrissnell.graywolf",
			Issues: []flareschema.CollectorIssue{{
				Kind: "launchctl_failed", Message: strings.TrimSpace(string(out)),
			}},
		}
	}
	body := string(out)
	return flareschema.ServiceStatus{
		Manager:  "launchd",
		Unit:     "com.chrissnell.graywolf",
		IsActive: strings.Contains(body, "state = running"),
		IsFailed: strings.Contains(body, "state = exited"),
	}
}
