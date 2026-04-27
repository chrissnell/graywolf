//go:build !linux && !darwin

package diagcollect

import "github.com/chrissnell/graywolf/pkg/flareschema"

// CollectServiceStatus on platforms without a known supervisor
// (today: Windows, BSDs) returns a not_supported issue.
func CollectServiceStatus() flareschema.ServiceStatus {
	return notSupportedServiceStatus("service supervisor probe not implemented for this OS")
}
