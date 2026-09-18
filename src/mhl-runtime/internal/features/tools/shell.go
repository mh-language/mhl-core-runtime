// Package tools implements native runtime tools.
package tools

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Result contains captured subprocess output and its exit status.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Cmd is the command execution tool.
type Cmd struct {
	// Stdout, when set, receives a copy of the subprocess's stdout as it
	// arrives — alongside (not instead of) the buffer captured into
	// Result.Stdout — so a caller can stream output (e.g. append it to a log
	// file incrementally) instead of only seeing it once the process exits.
	Stdout io.Writer

	// Stdin, when non-empty, is written to the subprocess's standard input
	// in full before Exec waits for it to exit. This is how a caller (e.g.
	// an agent's `stdin:` property) hands the child process large content —
	// a prompt, a schema — without it ever becoming part of argv, which on
	// Windows is capped at ~32,767 characters by CreateProcess regardless of
	// how much memory is available. Empty leaves the child's stdin attached
	// to the null device, unchanged from before this field existed.
	Stdin string
}

// Exec starts a command in its own process group and kills that group when the
// context is cancelled, including descendants spawned by the command.
func (c Cmd) Exec(ctx context.Context, name string, args ...string) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := checkCommandLineLength(name, args); err != nil {
		return Result{}, fmt.Errorf("tools: %w", err)
	}
	command := exec.Command(name, args...)
	configureProcessGroup(command)
	if c.Stdin != "" {
		command.Stdin = strings.NewReader(c.Stdin)
	}
	var stdout, stderr captureBuffer
	var stdoutWriter io.Writer = &stdout
	if c.Stdout != nil {
		stdoutWriter = io.MultiWriter(&stdout, c.Stdout)
	}
	command.Stdout, command.Stderr = stdoutWriter, &stderr
	if err := command.Start(); err != nil {
		return Result{}, fmt.Errorf("tools: start %q: %w", name, err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return commandResult(command, stdout.String(), stderr.String(), err), err
	case <-ctx.Done():
		killProcessGroup(command)
		err := <-done
		result := commandResult(command, stdout.String(), stderr.String(), err)
		return result, fmt.Errorf("tools: command %q cancelled: %w", name, ctx.Err())
	}
}

// Exec is the package-level convenience form of Cmd.Exec.
func Exec(ctx context.Context, name string, args ...string) (Result, error) {
	return (Cmd{}).Exec(ctx, name, args...)
}

func commandResult(command *exec.Cmd, stdout, stderr string, err error) Result {
	code := 0
	if command.ProcessState != nil {
		code = command.ProcessState.ExitCode()
	}
	_ = err
	return Result{Stdout: stdout, Stderr: stderr, ExitCode: code}
}

type captureBuffer struct{ data []byte }

func (b *captureBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *captureBuffer) String() string { return string(b.data) }
