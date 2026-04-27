package diagcollect

import (
	"strconv"
	"strings"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// parseSystemctlIsActive returns true when systemctl reported
// "active" (anything else — inactive, failed, empty, error — is false).
func parseSystemctlIsActive(stdout string) bool {
	return strings.TrimSpace(stdout) == "active"
}

// parseSystemctlNRestarts parses `systemctl show -p NRestarts`
// output ("NRestarts=N"). Anything unparseable returns 0.
func parseSystemctlNRestarts(stdout string) int {
	for _, line := range strings.Split(stdout, "\n") {
		if !strings.HasPrefix(line, "NRestarts=") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "NRestarts="))
		if val == "" {
			return 0
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// notSupportedServiceStatus is the platform-stub used by Windows
// and any future OS that lacks a known service supervisor.
func notSupportedServiceStatus(reason string) flareschema.ServiceStatus {
	return flareschema.ServiceStatus{
		Issues: []flareschema.CollectorIssue{{
			Kind:    "service_supervisor_unavailable",
			Message: reason,
		}},
	}
}
