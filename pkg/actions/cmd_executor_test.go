package actions

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func absTestData(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("posix shell test fixtures")
	}
	abs, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestCmdExecutorHappyPath(t *testing.T) {
	exe := NewCommandExecutor()
	a := &configstore.Action{Name: "Echo", Type: "command", CommandPath: absTestData(t, "echo.sh"), TimeoutSec: 5}
	req := ExecRequest{
		Action: a,
		Invocation: Invocation{
			ActionName:  a.Name,
			SenderCall:  "NW5W-7",
			Source:      SourceRF,
			OTPVerified: true,
			Args:        []KeyValue{{Key: "k1", Value: "v1"}},
		},
		Timeout: 5 * time.Second,
	}
	res := exe.Execute(context.Background(), req)
	if res.Status != StatusOK {
		t.Fatalf("status=%v detail=%q out=%q", res.Status, res.StatusDetail, res.OutputCapture)
	}
	if !strings.Contains(res.OutputCapture, "action=Echo") || !strings.Contains(res.OutputCapture, "sender=NW5W-7") {
		t.Fatalf("env not propagated: %q", res.OutputCapture)
	}
	if !strings.Contains(res.OutputCapture, "k1=v1") {
		t.Fatalf("argv not propagated: %q", res.OutputCapture)
	}
}

func TestCmdExecutorTimeout(t *testing.T) {
	exe := NewCommandExecutor()
	a := &configstore.Action{Name: "S", Type: "command", CommandPath: absTestData(t, "sleep.sh"), TimeoutSec: 1}
	res := exe.Execute(context.Background(), ExecRequest{
		Action:     a,
		Invocation: Invocation{ActionName: a.Name},
		Timeout:    1 * time.Second,
	})
	if res.Status != StatusTimeout {
		t.Fatalf("expected timeout, got %v %q", res.Status, res.StatusDetail)
	}
}

func TestCmdExecutorOutputCap(t *testing.T) {
	exe := NewCommandExecutor()
	a := &configstore.Action{Name: "Spam", Type: "command", CommandPath: absTestData(t, "spam.sh"), TimeoutSec: 5}
	res := exe.Execute(context.Background(), ExecRequest{
		Action: a, Invocation: Invocation{ActionName: a.Name}, Timeout: 5 * time.Second,
	})
	if res.Status != StatusOK {
		t.Fatalf("status=%v detail=%q", res.Status, res.StatusDetail)
	}
	if got := len(res.OutputCapture); got != cmdOutputCap {
		t.Fatalf("OutputCapture len = %d, want %d (cap)", got, cmdOutputCap)
	}
}

func TestBuildArgvFreeformShape(t *testing.T) {
	inv := Invocation{
		ActionName:  "sms",
		SenderCall:  "KE0XYZ",
		OTPVerified: true,
		Args:        []KeyValue{{Key: FreeformArgKey, Value: "+15555551212 hello world"}},
	}
	got := buildArgv(inv)
	want := []string{"sms", "KE0XYZ", "true", "+15555551212 hello world"}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildArgvKVShapeUnchanged(t *testing.T) {
	inv := Invocation{
		ActionName:  "echo",
		SenderCall:  "KE0XYZ",
		OTPVerified: false,
		Args:        []KeyValue{{Key: "msg", Value: "hi"}, {Key: "to", Value: "+1"}},
	}
	got := buildArgv(inv)
	want := []string{"echo", "KE0XYZ", "false", "msg=hi", "to=+1"}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildEnvFreeform(t *testing.T) {
	req := ExecRequest{
		Invocation: Invocation{
			ActionName:  "sms",
			SenderCall:  "KE0XYZ",
			OTPVerified: true,
			Args:        []KeyValue{{Key: FreeformArgKey, Value: "+15555551212 hello"}},
		},
	}
	env := buildEnv(req)
	var sawArg bool
	for _, kv := range env {
		if kv == "GW_ARG=+15555551212 hello" {
			sawArg = true
		}
		if strings.HasPrefix(kv, "GW_ARG_ARG=") {
			t.Fatalf("freeform must not double-name as GW_ARG_ARG: %q", kv)
		}
	}
	if !sawArg {
		t.Fatalf("expected GW_ARG=... in env, got %v", env)
	}
}

func TestBuildEnvKVUnchanged(t *testing.T) {
	req := ExecRequest{
		Invocation: Invocation{
			ActionName: "echo",
			Args:       []KeyValue{{Key: "msg", Value: "hi"}, {Key: "to", Value: "+1"}},
		},
	}
	env := buildEnv(req)
	want := map[string]bool{"GW_ARG_MSG=hi": false, "GW_ARG_TO=+1": false}
	for _, kv := range env {
		if _, ok := want[kv]; ok {
			want[kv] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Fatalf("missing %q in env: %v", k, env)
		}
	}
}

func TestCmdExecutorNonZero(t *testing.T) {
	exe := NewCommandExecutor()
	a := &configstore.Action{Name: "F", Type: "command", CommandPath: absTestData(t, "fail.sh"), TimeoutSec: 5}
	res := exe.Execute(context.Background(), ExecRequest{
		Action: a, Invocation: Invocation{ActionName: a.Name}, Timeout: 5 * time.Second,
	})
	if res.Status != StatusError {
		t.Fatalf("expected error, got %v", res.Status)
	}
	if res.ExitCode == nil || *res.ExitCode != 7 {
		t.Fatalf("expected exit 7, got %v", res.ExitCode)
	}
}
