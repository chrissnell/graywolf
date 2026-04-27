//go:build linux

package diagcollect

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/flareschema"
	"github.com/chrissnell/graywolf/pkg/logbuffer"
)

func osIdentity() (osIdentityResult, []flareschema.CollectorIssue) {
	var r osIdentityResult
	var issues []flareschema.CollectorIssue

	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		r.OSPretty = parseOSReleasePretty(data)
	} else {
		issues = append(issues, flareschema.CollectorIssue{
			Kind: "os_release_unreadable", Message: err.Error(), Path: "/etc/os-release",
		})
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		r.Kernel = strings.TrimSpace(string(out))
	}

	r.IsRaspberryPi = logbuffer.IsRaspberryPiHost()
	if r.IsRaspberryPi {
		if data, err := os.ReadFile("/sys/firmware/devicetree/base/model"); err == nil {
			r.PiModel = strings.TrimRight(strings.TrimSpace(string(data)), "\x00")
		}
	}

	r.NTPSynchronized = ntpSyncedLinux(&issues)
	r.UdevRulesPresent = udevRulesLinux(&issues)
	r.Groups = currentUserGroupsLinux(&issues)

	return r, issues
}

func ntpSyncedLinux(issues *[]flareschema.CollectorIssue) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "timedatectl", "show", "-p", "NTPSynchronized", "--value").Output()
	if err != nil {
		*issues = append(*issues, flareschema.CollectorIssue{
			Kind: "ntp_check_failed", Message: err.Error(),
		})
		return false
	}
	return strings.TrimSpace(string(out)) == "yes"
}

func udevRulesLinux(issues *[]flareschema.CollectorIssue) []string {
	const root = "/etc/udev/rules.d"
	entries, err := os.ReadDir(root)
	if err != nil {
		*issues = append(*issues, flareschema.CollectorIssue{
			Kind: "udev_rules_unreadable", Message: err.Error(), Path: root,
		})
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".rules") {
			continue
		}
		full := root + "/" + name
		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(body)), "graywolf") {
			out = append(out, name)
		}
	}
	return out
}

func currentUserGroupsLinux(issues *[]flareschema.CollectorIssue) []string {
	u, err := user.Current()
	if err != nil {
		*issues = append(*issues, flareschema.CollectorIssue{
			Kind: "user_lookup_failed", Message: err.Error(),
		})
		return nil
	}
	gids, err := u.GroupIds()
	if err != nil {
		*issues = append(*issues, flareschema.CollectorIssue{
			Kind: "groups_lookup_failed", Message: err.Error(),
		})
		return nil
	}
	out := make([]string, 0, len(gids))
	for _, gid := range gids {
		if g, gerr := user.LookupGroupId(gid); gerr == nil {
			out = append(out, g.Name)
		} else {
			out = append(out, gid)
		}
	}
	return out
}
