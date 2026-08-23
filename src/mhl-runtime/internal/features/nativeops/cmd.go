// Package nativeops implements the fixed cmd/git/fs/http operations a
// `tool` method body can call (language-design.md §7 "Ferramentas
// Nativas"), e.g. `cmd.exec(...)`, `git.diff(...)`, `fs.read(...)`,
// `http.post(...)`. Each function here is deliberately thin: it holds no
// MHL-specific concepts (no AST, no Env) — internal/engine/interpreter/tool.go is what
// evaluates a tool method's arguments and calls into this package.
package nativeops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/features/tools"
)

// Exec runs command — split on whitespace, no shell quoting/expansion, the
// same simplicity internal/tools.Cmd.Exec already has (it takes a bare
// name plus args, not a shell string) — and returns its captured output as
// a structured value: {"stdout", "stderr", "exit_code"}.
//
// A non-zero exit is not itself an error: the caller (a .mh pipeline)
// inspects exit_code, the same way an `agent.run` caller never sees a raw
// exec.ExitError either. Only a genuine failure to execute at all (bad
// binary, timeout) errors. timeout <= 0 means no deadline.
func Exec(ctx context.Context, command string, timeout time.Duration) (map[string]any, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, fmt.Errorf("cmd.exec: command must not be empty")
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	result, err := tools.Exec(ctx, fields[0], fields[1:]...)
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("cmd.exec %q: %w", command, err)
	}
	return map[string]any{
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
		"exit_code": float64(result.ExitCode),
	}, nil
}

// ExecArgs runs argv[0] with argv[1:] as its arguments, with no whitespace
// splitting or shell quoting — unlike Exec, each element is passed through
// to exec.Command verbatim, so an argument containing spaces (e.g. a git
// commit message) survives intact. timeout <= 0 means no deadline.
func ExecArgs(ctx context.Context, argv []string, timeout time.Duration) (map[string]any, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("cmd.exec: argv must not be empty")
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	result, err := tools.Exec(ctx, argv[0], argv[1:]...)
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("cmd.exec %q: %w", argv, err)
	}
	return map[string]any{
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
		"exit_code": float64(result.ExitCode),
	}, nil
}

// ExecAll runs each argv in commands concurrently — one goroutine per
// command, the same per-child process-group isolation Exec/ExecArgs
// already have via tools.Exec — and returns their results in the same
// order as commands, not completion order, so a caller can zip result[i]
// back to commands[i] (e.g. lint/typecheck/tests as independent verify
// gates that all fan out and get AND'd together, the diamond pattern
// Flows.Development's TryParallelConfiguredVerify uses). timeout applies
// per command, not to the whole batch — commands don't share a deadline.
//
// A non-zero exit in any command is not itself an error (same philosophy
// as Exec/ExecArgs: the caller inspects each result's exit_code); only a
// genuine failure to launch some command aborts the whole batch, since
// that's an infra/config problem, not a normal "verify failed" outcome.
func ExecAll(ctx context.Context, commands [][]string, timeout time.Duration) ([]map[string]any, error) {
	results := make([]map[string]any, len(commands))
	errs := make([]error, len(commands))
	var wg sync.WaitGroup
	for i, argv := range commands {
		wg.Add(1)
		go func(i int, argv []string) {
			defer wg.Done()
			result, err := ExecArgs(ctx, argv, timeout)
			results[i] = result
			errs[i] = err
		}(i, argv)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("cmd.exec_all[%d]: %w", i, err)
		}
	}
	return results, nil
}
