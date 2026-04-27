//go:build linux

package diagcollect

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// CollectGPIO walks /dev/gpiochip* and produces one PTTCandidate per
// chip with kind "gpio_chip". Read-only: never opens the chip for
// write, never toggles lines.
func CollectGPIO() ([]flareschema.PTTCandidate, []flareschema.CollectorIssue) {
	matches, err := filepath.Glob("/dev/gpiochip*")
	if err != nil {
		return nil, []flareschema.CollectorIssue{{
			Kind: "gpio_glob_failed", Message: err.Error(),
		}}
	}
	var (
		out    []flareschema.PTTCandidate
		issues []flareschema.CollectorIssue
	)
	for _, dev := range matches {
		base := filepath.Base(dev)
		sysfs := filepath.Join("/sys/class/gpio", base)

		label := ""
		if data, lerr := os.ReadFile(filepath.Join(sysfs, "label")); lerr == nil {
			label = parseSysfsLabel(string(data))
		} else if !os.IsNotExist(lerr) {
			issues = append(issues, flareschema.CollectorIssue{
				Kind: "gpio_label_unreadable", Message: lerr.Error(), Path: sysfs + "/label",
			})
		}
		ngpio := 0
		if data, lerr := os.ReadFile(filepath.Join(sysfs, "ngpio")); lerr == nil {
			ngpio = parseSysfsNGPIO(string(data))
		}

		stat, sErr := os.Stat(dev)
		desc := label
		if ngpio > 0 {
			desc = fmt.Sprintf("%s (%d lines)", label, ngpio)
		}

		cand := flareschema.PTTCandidate{
			Kind:        "gpio_chip",
			Path:        dev,
			Description: desc,
		}
		if sErr == nil {
			cand.Permissions = stat.Mode().String()
			if sys, ok := stat.Sys().(*syscall.Stat_t); ok {
				cand.Owner = strconv.FormatUint(uint64(sys.Uid), 10)
				cand.Group = strconv.FormatUint(uint64(sys.Gid), 10)
			}
		} else {
			issues = append(issues, flareschema.CollectorIssue{
				Kind: "gpio_stat_failed", Message: sErr.Error(), Path: dev,
			})
		}
		out = append(out, cand)
	}
	return out, issues
}

func parseSysfsLabel(s string) string {
	return strings.TrimRight(s, "\n\x00")
}

func parseSysfsNGPIO(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
