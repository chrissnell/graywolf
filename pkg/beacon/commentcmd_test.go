package beacon

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSplitArgv_Basic(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"uptime", []string{"uptime"}},
		{"echo hello world", []string{"echo", "hello", "world"}},
		{`echo "hello world"`, []string{"echo", "hello world"}},
		{`echo 'a b' c`, []string{"echo", "a b", "c"}},
		{`echo foo\ bar`, []string{"echo", "foo bar"}},
		{`echo ""`, []string{"echo", ""}},
	}
	for _, c := range cases {
		got, err := SplitArgv(c.in)
		if err != nil {
			t.Errorf("split %q: %v", c.in, err)
			continue
		}
		if !sliceEq(got, c.want) {
			t.Errorf("split %q = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSplitArgv_Unterminated(t *testing.T) {
	if _, err := SplitArgv(`echo "oops`); err == nil {
		t.Errorf("expected error on unterminated quote")
	}
}

func TestRunCommentCmd_StdoutCaptured(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only echo semantics")
	}
	out, err := RunCommentCmd(context.Background(), []string{"echo", "hello graywolf"}, time.Second)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "hello graywolf" {
		t.Errorf("stdout = %q", out)
	}
}

// Critical security test: shell metacharacters in argv MUST be treated as
// literal arguments and NOT executed as shell commands. A successful
// injection would run `rm`/`whoami`/`id`; instead we expect echo to print
// the raw string.
func TestRunCommentCmd_NoShellInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only echo semantics")
	}
	payloads := []string{
		"; rm -rf /",
		"$(whoami)",
		"`id`",
		"| cat /etc/passwd",
		"&& echo owned",
	}
	for _, p := range payloads {
		out, err := RunCommentCmd(context.Background(), []string{"echo", p}, 2*time.Second)
		if err != nil {
			t.Errorf("run %q: %v", p, err)
			continue
		}
		if !strings.Contains(out, p) {
			t.Errorf("payload %q not echoed literally; got %q", p, out)
		}
		// Canary: the string must be exactly the literal payload (trimmed).
		if out != p {
			t.Errorf("unexpected transformation: in=%q out=%q", p, out)
		}
	}
}

func TestRunCommentCmd_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only sleep")
	}
	start := time.Now()
	_, err := RunCommentCmd(context.Background(), []string{"sleep", "5"}, 100*time.Millisecond)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("timeout not enforced: %v", elapsed)
	}
}

func TestRunCommentCmd_MissingProgram(t *testing.T) {
	_, err := RunCommentCmd(context.Background(), []string{"definitely-not-a-real-binary-xyz"}, time.Second)
	if err == nil {
		t.Errorf("expected error for missing binary")
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
