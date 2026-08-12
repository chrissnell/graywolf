package main

import (
	"log/slog"
	"syscall"
)

// ES_SYSTEM_REQUIRED | ES_CONTINUOUS: prevent system sleep; release on cleanup.
const (
	esSystemRequired uint32 = 0x00000001
	esContinuous     uint32 = 0x80000000
)

// preventSystemSleep calls SetThreadExecutionState to prevent system sleep.
func preventSystemSleep(logger *slog.Logger) func() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setThreadExecState := kernel32.NewProc("SetThreadExecutionState")
	ret, _, _ := setThreadExecState.Call(uintptr(esContinuous | esSystemRequired))
	if ret == 0 {
		logger.Warn("preventSystemSleep: SetThreadExecutionState failed")
		return func() {}
	}
	logger.Info("preventSystemSleep: system sleep disabled (SetThreadExecutionState)")
	return func() {
		// Restore default by clearing ES_SYSTEM_REQUIRED.
		setThreadExecState.Call(uintptr(esContinuous))
	}
}
