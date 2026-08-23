// Package tools implements native runtime tools.
package tools

import (
	"context"
	"fmt"
	"os/exec"
)

// Result contains captured subprocess output and its exit status.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Cmd is the command execution tool.
type Cmd struct{}

// Exec starts a command in its own process group and kills that group when the
// context is cancelled, including descendants spawned by the command.
func (Cmd) Exec(ctx context.Context, name string, args ...string) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	command := exec.Command(name, args...)
	configureProcessGroup(command)
	var stdout, stderr captureBuffer
	command.Stdout, command.Stderr = &stdout, &stderr
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
