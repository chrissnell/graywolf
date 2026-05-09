package platformproto

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestProtoBindingsDoNotDrift regenerates platform.pb.go in place via
// `make proto` and asserts `git diff --quiet -- pkg/platformproto/`.
// Tool-version-tolerant: protoc-gen-go minor versions produce textually
// different output (header preamble, field formatting), and a byte-exact
// comparison would fire on cosmetic tool drift instead of real schema
// drift. `git diff` only fires when the regenerated file actually differs
// from what's committed — i.e. proto schema or generator semantics
// changed. Skipped if `make` / `protoc` / `protoc-gen-go` are absent so
// dev hosts without the toolchain can still run `go test ./...`.
//
// On exit the test always restores the working tree via
// `git checkout -- pkg/platformproto/platform.pb.go` so a successful run
// leaves no diff and a failure leaves no stranded modifications either.
func TestProtoBindingsDoNotDrift(t *testing.T) {
	for _, tool := range []string{"make", "protoc", "protoc-gen-go", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH; install proto toolchain to run drift guard", tool)
		}
	}

	_, thisFile, _, _ := runtime.Caller(0)
	pkgDir := filepath.Dir(thisFile)
	repoRoot := filepath.Dir(filepath.Dir(pkgDir))

	t.Cleanup(func() {
		// Always restore — failure or success.
		restore := exec.Command("git", "checkout", "--", "pkg/platformproto/platform.pb.go")
		restore.Dir = repoRoot
		_ = restore.Run()
	})

	regen := exec.Command("make", "proto")
	regen.Dir = repoRoot
	regen.Stderr = new(bytes.Buffer)
	regen.Stdout = regen.Stderr
	if err := regen.Run(); err != nil {
		t.Fatalf("make proto failed: %v\noutput:\n%s", err, regen.Stderr)
	}

	diff := exec.Command("git", "diff", "--quiet", "--", "pkg/platformproto/")
	diff.Dir = repoRoot
	if err := diff.Run(); err != nil {
		// Non-zero exit = working-tree differs from index = drift.
		t.Fatal("pkg/platformproto/ is out of date with proto/platform.proto. Run `make proto` and commit the result.")
	}
}
