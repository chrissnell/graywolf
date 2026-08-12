package main

import (
	"log/slog"
	"os/exec"
)

// preventSystemSleep invokes systemd-inhibit to block idle and system sleep.
// Falls back silently on non-systemd distros (Alpine, OpenRC, etc.).
func preventSystemSleep(logger *slog.Logger) func() {
	cmd := exec.Command("systemd-inhibit",
		"--what=idle:sleep",
		"--who=graywolf",
		"--why=APRS station running",
		"--mode=block",
		"sleep", "infinity",
	)
	if err := cmd.Start(); err != nil {
		logger.Warn("preventSystemSleep: systemd-inhibit not available", "err", err)
		return func() {}
	}
	logger.Info("preventSystemSleep: system sleep disabled (systemd-inhibit)")
	return func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}
