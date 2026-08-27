package nativeops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/features/tools"
)

// gitArgs prefixes rest with `-C <dir>` when dir is non-empty, so every
// git.* op can run against a directory other than the interpreter's working
// directory without that op needing to know how `git -C` is spelled. An
// empty dir means "run in the current working directory", the pre-`dir:`
// behavior.
func gitArgs(dir string, rest ...string) []string {
	if strings.TrimSpace(dir) == "" {
		return rest
	}
	return append([]string{"-C", dir}, rest...)
}

// Diff runs `git [-C dir] diff [target]` and returns its stdout as plain
// text — unlike Exec, this has no structured result for a caller to inspect
// a failure through (matching language-design.md §8's usage, where the
// result is stored/read as if it were just the diff text: `diff.is_empty()`,
// `session_mem.set("current_diff", diff)`), so a non-zero exit here is a
// real, fail-closed error. dir may be "" to run in the current directory.
func Diff(ctx context.Context, target, dir string) (string, error) {
	args := gitArgs(dir, "diff")
	if target != "" {
		args = append(args, target)
	}
	result, err := tools.Exec(ctx, "git", args...)
	if err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("git diff: %s", detail)
	}
	return result.Stdout, nil
}

// Add stages paths (including new/removed files under them) via
// `git [-C dir] add -A -- <paths...>` — the shape a handoff step needs (stage
// everything under the target directory while excluding, say, a
// `.harness` bookkeeping folder: `Add(ctx, []string{".", ":(exclude).harness"}, dir)`).
// Returns the same structured shape as cmd.exec, {"stdout","stderr","exit_code"}:
// unlike Diff, a non-zero exit here is not itself an error — the caller
// inspects exit_code, the same way it would for cmd.exec. dir may be "".
func Add(ctx context.Context, paths []string, dir string) (map[string]any, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("git add: at least one path is required")
	}
	args := gitArgs(dir, append([]string{"add", "-A", "--"}, paths...)...)
	return runGit(ctx, args)
}

// Commit runs `git [-C dir] commit -m message [-- paths...]`. message is
// passed as its own exec argument — never split or shell-quoted — so a
// message containing spaces survives intact; this is the operation Fase
// 0.2's cmd.exec argv form was a prerequisite for. paths may be empty for a
// commit that takes whatever is already staged. dir may be "".
func Commit(ctx context.Context, message string, paths []string, dir string) (map[string]any, error) {
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("git commit: message must not be empty")
	}
	rest := []string{"commit", "-m", message}
	if len(paths) > 0 {
		rest = append(rest, "--")
		rest = append(rest, paths...)
	}
	return runGit(ctx, gitArgs(dir, rest...))
}

// Status runs `git [-C dir] status --short [-- paths...]`. dir may be "".
func Status(ctx context.Context, paths []string, dir string) (map[string]any, error) {
	rest := []string{"status", "--short"}
	if len(paths) > 0 {
		rest = append(rest, "--")
		rest = append(rest, paths...)
	}
	return runGit(ctx, gitArgs(dir, rest...))
}

// RevParse runs `git [-C dir] rev-parse --short ref` and returns its trimmed
// stdout — mirrors Diff: plain text, fail-closed on a non-zero exit, since
// there is no "expected failure" for a caller to branch on here (a bad ref
// is a genuine error, not a normal outcome like a non-empty diff). dir may
// be "".
func RevParse(ctx context.Context, ref, dir string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("git rev-parse: ref must not be empty")
	}
	result, err := tools.Exec(ctx, "git", gitArgs(dir, "rev-parse", "--short", ref)...)
	if err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("git rev-parse: %s", detail)
	}
	return strings.TrimSpace(result.Stdout), nil
}

// Log runs `git [-C dir] log -n n --oneline` and returns its stdout as plain
// text — mirrors Diff. dir may be "".
func Log(ctx context.Context, n int, dir string) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("git log: n must be positive")
	}
	result, err := tools.Exec(ctx, "git", gitArgs(dir, "log", "-n", strconv.Itoa(n), "--oneline")...)
	if err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("git log: %s", detail)
	}
	return result.Stdout, nil
}

// runGit executes a git subcommand and reports it the same way cmd.Exec
// does: a non-zero exit is a normal, inspectable outcome
// ({"stdout","stderr","exit_code"}), not a Go error — only a genuine
// failure to run git at all is.
func runGit(ctx context.Context, args []string) (map[string]any, error) {
	result, err := tools.Exec(ctx, "git", args...)
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return map[string]any{
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
		"exit_code": float64(result.ExitCode),
	}, nil
}
