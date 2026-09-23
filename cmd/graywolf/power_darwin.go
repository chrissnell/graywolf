package main

import (
	"log/slog"
	"os/exec"
)

// preventSystemSleep spawns caffeinate to block idle and AC-power system sleep.
func preventSystemSleep(logger *slog.Logger) func() {
	// -i: prevent idle sleep; -s: prevent system sleep on AC power.
	cmd := exec.Command("caffeinate", "-is")
	if err := cmd.Start(); err != nil {
		logger.Warn("preventSystemSleep: caffeinate failed to start", "err", err)
		return func() {}
	}
	logger.Info("preventSystemSleep: system sleep disabled (caffeinate -is)")
	return func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}
