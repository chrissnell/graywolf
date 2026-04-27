//go:build linux

package diagcollect

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

const graywolfSystemdUnit = "graywolf.service"

// CollectServiceStatus runs three quick systemctl probes against
// graywolf.service. Missing systemctl produces a single
// service_supervisor_unavailable issue; any other failure becomes a
// per-probe issue with the others still populated where possible.
func CollectServiceStatus() flareschema.ServiceStatus {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return notSupportedServiceStatus("systemctl not on PATH")
	}
	out := flareschema.ServiceStatus{
		Manager: "systemd",
		Unit:    graywolfSystemdUnit,
	}
	if active, err := runSystemctl("is-active", graywolfSystemdUnit); err == nil {
		out.IsActive = parseSystemctlIsActive(active)
	} else {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind: "is_active_failed", Message: err.Error(),
		})
	}
	if failed, err := runSystemctl("is-failed", graywolfSystemdUnit); err == nil {
		// is-failed prints "failed" exactly when the unit is failed.
		// Treat any other reply as not-failed.
		out.IsFailed = strings.TrimSpace(failed) == "failed"
	} else {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind: "is_failed_failed", Message: err.Error(),
		})
	}
	if show, err := runSystemctl("show", "-p", "NRestarts", graywolfSystemdUnit); err == nil {
		out.RestartCount = parseSystemctlNRestarts(show)
	} else {
		out.Issues = append(out.Issues, flareschema.CollectorIssue{
			Kind: "nrestarts_failed", Message: err.Error(),
		})
	}
	return out
}

// runSystemctl runs systemctl with the given args and returns its
// stdout. Non-zero exits (which systemctl uses for is-active=inactive
// etc.) are mapped to a successful return so the parser sees the
// stdout text — only true execution errors (missing binary, timeout)
// produce err != nil.
func runSystemctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return string(out), nil
		}
		return "", err
	}
	return string(out), nil
}
