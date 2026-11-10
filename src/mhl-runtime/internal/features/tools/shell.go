// Package tools implements native runtime tools.
package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// ErrTimeout is wrapped by the error Cmd.Exec returns when Cmd.Timeout
// elapsed before the command exited. It deliberately does not wrap
// context.DeadlineExceeded: a per-execution timeout is an ordinary failure a
// retry policy may choose to repeat (traffic.Retrier never repeats a
// cancelled/expired context, which is reserved for run- and step-level
// deadlines that must stop everything).
var ErrTimeout = errors.New("timeout")

// DefaultKillGrace is how long a timed-out command is given, between the
// polite termination request and the forced kill, to flush its final state —
// the value callers use when their configuration says nothing.
const DefaultKillGrace = 10 * time.Second

// pipeDrainDelay bounds how long Exec waits for the stdout/stderr pipes to
// close once the command itself has exited (exec.Cmd.WaitDelay). A
// descendant that escaped the process group (setsid — e.g. Node's
// `detached: true`) can hold them open indefinitely, which would otherwise
// block Wait forever even after the whole group was killed. Only applied when
// Cmd.Timeout is set, so a command without one keeps its historical
// wait-for-every-writer behavior. A var so tests can shorten it.
var pipeDrainDelay = 5 * time.Second

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

	// Dir, when non-empty, is the working directory the subprocess starts
	// in (exec.Cmd.Dir — a plain chdir in the child on every platform, no
	// shell involved). Empty inherits the caller's.
	Dir string

	// Stdin, when non-empty, is written to the subprocess's standard input
	// in full before Exec waits for it to exit. This is how a caller (e.g.
	// an agent's `stdin:` property) hands the child process large content —
	// a prompt, a schema — without it ever becoming part of argv, which on
	// Windows is capped at ~32,767 characters by CreateProcess regardless of
	// how much memory is available. Empty leaves the child's stdin attached
	// to the null device, unchanged from before this field existed.
	Stdin string

	// Timeout, when positive, bounds this one execution. When it elapses the
	// whole process tree is asked to terminate (SIGTERM to the process group
	// on Unix), given KillGrace to exit on its own, then killed (SIGKILL to
	// the group; on Windows the tree is terminated immediately, there being
	// no console-less equivalent of SIGTERM). Exec then returns an error
	// wrapping ErrTimeout — whatever the command's own exit code was — along
	// with the output captured up to that point. Zero means no limit.
	Timeout time.Duration

	// KillGrace is the wait between the termination request and the forced
	// kill after Timeout elapses. Zero kills immediately; callers that want
	// the default pass DefaultKillGrace.
	KillGrace time.Duration
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
	command.Dir = c.Dir
	if c.Stdin != "" {
		command.Stdin = strings.NewReader(c.Stdin)
	}
	var stdout, stderr captureBuffer
	var stdoutWriter io.Writer = &stdout
	if c.Stdout != nil {
		stdoutWriter = io.MultiWriter(&stdout, c.Stdout)
	}
	command.Stdout, command.Stderr = stdoutWriter, &stderr
	if c.Timeout > 0 {
		command.WaitDelay = pipeDrainDelay
	}
	if err := command.Start(); err != nil {
		return Result{}, fmt.Errorf("tools: start %q: %w", name, err)
	}
	tree := attachProcessTree(command)
	defer tree.release()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	var timeoutC <-chan time.Time
	if c.Timeout > 0 {
		timer := time.NewTimer(c.Timeout)
		defer timer.Stop()
		timeoutC = timer.C
	}
	select {
	case err := <-done:
		err = ignorePipeDrain(err)
		return commandResult(command, stdout.String(), stderr.String(), err), err
	case <-ctx.Done():
		tree.kill()
		err := <-done
		result := commandResult(command, stdout.String(), stderr.String(), err)
		return result, fmt.Errorf("tools: command %q cancelled: %w", name, ctx.Err())
	case <-timeoutC:
		err := c.stopAfterTimeout(ctx, tree, done)
		result := commandResult(command, stdout.String(), stderr.String(), err)
		return result, fmt.Errorf("tools: command %q: %w after %s", name, ErrTimeout, FormatDuration(c.Timeout))
	}
}

// stopAfterTimeout terminates a timed-out command's process tree: a polite
// request first (when the platform has one and KillGrace allows it), then a
// forced kill once the grace elapses, the command exits, or ctx ends. The
// forced kill is sent even when the command exited on its own during the
// grace, so descendants that ignored the request (still in the group) don't
// outlive it. Returns Wait's error.
func (c Cmd) stopAfterTimeout(ctx context.Context, tree *processTree, done <-chan error) error {
	if c.KillGrace <= 0 || !tree.terminate() {
		tree.kill()
		return <-done
	}
	grace := time.NewTimer(c.KillGrace)
	defer grace.Stop()
	select {
	case err := <-done:
		tree.kill()
		return err
	case <-grace.C:
	case <-ctx.Done():
	}
	tree.kill()
	return <-done
}

// ignorePipeDrain treats exec.ErrWaitDelay — the command exited successfully
// but a descendant still held its output pipes after pipeDrainDelay — as
// success: the command itself finished, and the output it wrote is captured.
func ignorePipeDrain(err error) error {
	if errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}
	return err
}

// FormatDuration renders d the way a `.mh` duration literal is written
// (`30m`, `90s`, `1.5h`, `250ms`) rather than Go's `30m0s`, so an error
// message names the deadline exactly as the program declared it.
func FormatDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "0s"
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	case d%time.Second == 0:
		return fmt.Sprintf("%ds", d/time.Second)
	case d < time.Second:
		return fmt.Sprintf("%dms", d/time.Millisecond)
	}
	return d.String()
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
