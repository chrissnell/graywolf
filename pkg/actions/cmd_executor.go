package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const cmdOutputCap = 4 * 1024

// CommandExecutor runs a configured Action.CommandPath via os/exec —
// always argv-style, never `sh -c`. Stdout+stderr are merged and
// truncated to cmdOutputCap bytes for capture.
type CommandExecutor struct{}

func NewCommandExecutor() *CommandExecutor { return &CommandExecutor{} }

func (CommandExecutor) Execute(ctx context.Context, req ExecRequest) Result {
	a := req.Action
	if a == nil || a.CommandPath == "" {
		return Result{Status: StatusError, StatusDetail: "missing command_path"}
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv := buildArgv(req.Invocation)
	cmd := exec.CommandContext(runCtx, a.CommandPath, argv...)
	cmd.Env = append(cmd.Env, buildEnv(req)...)
	if a.WorkingDir != "" {
		cmd.Dir = a.WorkingDir
	} else {
		cmd.Dir = filepath.Dir(a.CommandPath)
	}
	// SIGTERM on cancel; CommandContext kills with SIGKILL by default,
	// so override Cancel for graceful first try and let WaitDelay kick in
	// after 2s for the SIGKILL escalation.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second

	var buf bytes.Buffer
	cmd.Stdout = &cappedWriter{w: &buf, max: cmdOutputCap}
	cmd.Stderr = cmd.Stdout

	startErr := cmd.Start()
	if startErr != nil {
		return Result{Status: StatusError, StatusDetail: fmt.Sprintf("start: %v", startErr)}
	}
	waitErr := cmd.Wait()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return Result{Status: StatusTimeout, StatusDetail: "timed out", OutputCapture: buf.String()}
	}
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			code := ee.ExitCode()
			return Result{
				Status:        StatusError,
				StatusDetail:  fmt.Sprintf("exit %d", code),
				ExitCode:      &code,
				OutputCapture: buf.String(),
			}
		}
		return Result{Status: StatusError, StatusDetail: waitErr.Error(), OutputCapture: buf.String()}
	}
	zero := 0
	return Result{Status: StatusOK, ExitCode: &zero, OutputCapture: buf.String()}
}

func buildArgv(inv Invocation) []string {
	out := []string{
		inv.ActionName,
		inv.SenderCall,
		boolStr(inv.OTPVerified),
	}
	for _, kv := range inv.Args {
		out = append(out, kv.Key+"="+kv.Value)
	}
	return out
}

func buildEnv(req ExecRequest) []string {
	inv := req.Invocation
	env := []string{
		"GW_ACTION_NAME=" + inv.ActionName,
		"GW_SENDER_CALL=" + inv.SenderCall,
		"GW_OTP_VERIFIED=" + boolStr(inv.OTPVerified),
		"GW_OTP_CRED_NAME=" + inv.OTPCredName,
		"GW_SOURCE=" + string(inv.Source),
		fmt.Sprintf("GW_INVOCATION_ID=%d", inv.ID),
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	for _, kv := range inv.Args {
		env = append(env, "GW_ARG_"+envSafe(kv.Key)+"="+kv.Value)
	}
	return env
}

func envSafe(k string) string {
	upper := strings.ToUpper(k)
	b := make([]byte, len(upper))
	for i := 0; i < len(upper); i++ {
		c := upper[i]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b[i] = c
		} else {
			b[i] = '_'
		}
	}
	return string(b)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type cappedWriter struct {
	w   io.Writer
	max int
	n   int
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.n >= c.max {
		return len(p), nil
	}
	room := c.max - c.n
	if len(p) > room {
		p = p[:room]
	}
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}
