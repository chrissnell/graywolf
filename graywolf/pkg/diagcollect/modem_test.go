package diagcollect

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveModemPath_ExplicitWins(t *testing.T) {
	got, err := resolveModemPathFrom("/explicit/graywolf-modem", "/usr/bin/graywolf", "", t.TempDir())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "/explicit/graywolf-modem" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveModemPath_EnvBeforeSibling(t *testing.T) {
	got, err := resolveModemPathFrom("", "/usr/bin/graywolf", "/env/graywolf-modem", t.TempDir())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "/env/graywolf-modem" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveModemPath_SiblingOfExe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "graywolf")
	sibling := filepath.Join(dir, "graywolf-modem")
	if err := os.WriteFile(exe, []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveModemPathFrom("", exe, "", dir)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Normalize both paths to handle symlinks (e.g., /var → /private/var on macOS).
	gotNorm, _ := filepath.EvalSymlinks(got)
	siblingNorm, _ := filepath.EvalSymlinks(sibling)
	if gotNorm != siblingNorm {
		t.Fatalf("got %q (norm: %q), want %q (norm: %q)", got, gotNorm, sibling, siblingNorm)
	}
}

func TestResolveModemPath_DevTargetRelease(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target", "release")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(target, "graywolf-modem")
	if err := os.WriteFile(bin, []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveModemPathFrom("", "", "", root)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != bin {
		t.Fatalf("got %q, want %q", got, bin)
	}
}

func TestResolveModemPath_NotFound(t *testing.T) {
	// Empty workdir + empty exe + empty env + no $PATH hit → error.
	t.Setenv("PATH", "/no/such/dir")
	_, err := resolveModemPathFrom("", "", "", t.TempDir())
	if err == nil {
		t.Fatal("err = nil, want not-found")
	}
}

func TestRunListing_BinaryMissing(t *testing.T) {
	// Use a path that definitely doesn't exist.
	out, issue := RunListing("/no/such/graywolf-modem", "--list-audio")
	if out != nil {
		t.Fatalf("out = %v, want nil", out)
	}
	if issue == nil || issue.Kind != "modem_unavailable" {
		t.Fatalf("issue = %+v, want kind=modem_unavailable", issue)
	}
}

func TestRunListing_NonZeroExit(t *testing.T) {
	// `false` returns 1; pretend it's the modem to exercise the
	// non-zero-exit branch without depending on a real cargo build.
	// Modern macOS keeps it at /usr/bin/false rather than /bin/false,
	// so resolve via PATH instead of hardcoding.
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Skipf("false not available on PATH: %v", err)
	}
	out, issue := RunListing(falseBin, "--list-audio")
	if out != nil {
		t.Fatalf("out = %v, want nil", out)
	}
	if issue == nil || issue.Kind != "modem_failed" {
		t.Fatalf("issue = %+v, want kind=modem_failed", issue)
	}
}

func TestRunListing_Success(t *testing.T) {
	// /bin/echo prints its args. Use it as a stand-in modem that
	// emits a known string we can assert against.
	echo, err := exec.LookPath("echo")
	if err != nil {
		t.Skipf("echo not on PATH: %v", err)
	}
	out, issue := RunListing(echo, `{"hosts":[]}`)
	if issue != nil {
		t.Fatalf("issue = %+v, want nil", issue)
	}
	if string(out) == "" {
		t.Fatalf("out empty")
	}
	_ = errors.New // silence import in tools that don't strip
}
