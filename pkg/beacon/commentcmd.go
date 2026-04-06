package beacon

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

// SplitArgv splits a command string into argv tokens, honoring single and
// double quotes and backslash escapes. This is a conservative
// shell-word-like splitter used ONLY when the configstore stores
// comment_cmd as a single string. Unlike sh, it does NOT expand
// variables, globs, backticks, or $(...) — metacharacters are treated as
// literal arguments. Empty string → zero args.
func SplitArgv(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	hasToken := false

	flush := func() {
		if hasToken {
			args = append(args, cur.String())
			cur.Reset()
			hasToken = false
		}
	}

	for _, r := range s {
		if escaped {
			cur.WriteRune(r)
			hasToken = true
			escaped = false
			continue
		}
		switch {
		case r == '\\' && !inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			hasToken = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			hasToken = true
		case unicode.IsSpace(r) && !inSingle && !inDouble:
			flush()
		default:
			cur.WriteRune(r)
			hasToken = true
		}
	}
	if inSingle || inDouble {
		return nil, errors.New("beacon: unterminated quote in comment_cmd")
	}
	if escaped {
		return nil, errors.New("beacon: trailing backslash in comment_cmd")
	}
	flush()
	return args, nil
}

// RunCommentCmd executes argv with the given timeout and returns the
// trimmed stdout. argv[0] is the program; subsequent entries are literal
// arguments (no shell interpretation). On non-zero exit, timeout, or
// missing program, an error is returned and captured stdout so far is
// also returned (possibly empty). The caller is expected to fall back to
// the static comment on error.
func RunCommentCmd(ctx context.Context, argv []string, timeout time.Duration) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("beacon: empty comment_cmd argv")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	// Discard stderr; slog caller can log the error status.
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(out.String()), err
	}
	// APRS comment should be single-line; collapse any newlines.
	trimmed := strings.TrimSpace(out.String())
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	trimmed = strings.ReplaceAll(trimmed, "\r", "")
	return trimmed, nil
}
