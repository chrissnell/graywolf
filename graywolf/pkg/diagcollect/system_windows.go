//go:build windows

package diagcollect

import (
	"os/user"
	"runtime"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// osIdentity on Windows surfaces the bare minimum: runtime.GOOS,
// process arch, and the running user's groups. Service supervisor
// state lives in the service collector. Everything else (Pi
// detection, NTP, udev) is Linux-specific and stays empty here with
// a single not_supported issue.
func osIdentity() (osIdentityResult, []flareschema.CollectorIssue) {
	var r osIdentityResult
	r.OSPretty = "Windows " + runtime.GOARCH

	if u, err := user.Current(); err == nil {
		if gids, gerr := u.GroupIds(); gerr == nil {
			for _, gid := range gids {
				if g, lerr := user.LookupGroupId(gid); lerr == nil {
					r.Groups = append(r.Groups, g.Name)
				}
			}
		}
	}

	return r, []flareschema.CollectorIssue{{
		Kind:    "not_supported",
		Message: "linux-only system probes (Pi detection, NTP, udev) skipped on windows",
	}}
}
