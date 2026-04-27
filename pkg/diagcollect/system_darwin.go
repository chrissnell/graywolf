//go:build darwin

package diagcollect

import (
	"os/exec"
	"os/user"
	"strings"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

func osIdentity() (osIdentityResult, []flareschema.CollectorIssue) {
	var r osIdentityResult
	var issues []flareschema.CollectorIssue

	if out, err := exec.Command("sw_vers", "-productName").Output(); err == nil {
		name := strings.TrimSpace(string(out))
		ver, _ := exec.Command("sw_vers", "-productVersion").Output()
		r.OSPretty = strings.TrimSpace(name + " " + strings.TrimSpace(string(ver)))
	} else {
		issues = append(issues, flareschema.CollectorIssue{
			Kind: "sw_vers_failed", Message: err.Error(),
		})
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		r.Kernel = strings.TrimSpace(string(out))
	}

	if u, err := user.Current(); err == nil {
		if gids, gerr := u.GroupIds(); gerr == nil {
			for _, gid := range gids {
				if g, lerr := user.LookupGroupId(gid); lerr == nil {
					r.Groups = append(r.Groups, g.Name)
				}
			}
		}
	}

	return r, issues
}
