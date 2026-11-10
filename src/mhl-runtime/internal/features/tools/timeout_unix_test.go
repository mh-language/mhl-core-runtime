//go:build !windows

package tools

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// processGone polls until pid no longer exists (a killed orphan is reaped by
// init/launchd asynchronously, and a zombie still answers kill(pid, 0)).
func processGone(t *testing.T, pid int) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func firstLinePID(t *testing.T, stdout string) int {
	t.Helper()
	line := strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0])
	pid, err := strconv.Atoi(line)
	if err != nil {
		t.Fatalf("expected a pid on stdout's first line, got %q", stdout)
	}
	return pid
}

func TestExecTimeoutFailsWithErrTimeout(t *testing.T) {
	start := time.Now()
	_, err := (Cmd{Timeout: 200 * time.Millisecond, KillGrace: 2 * time.Second}).Exec(context.Background(), "sh", "-c", "sleep 5")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v must not wrap context.DeadlineExceeded (the retrier would never repeat it)", err)
	}
	if !strings.Contains(err.Error(), "timeout after 200ms") {
		t.Errorf("err = %q, want it to name the deadline", err)
	}
	// sleep dies on SIGTERM at once, well before the 2s grace.
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Errorf("took %s, want ~200ms (SIGTERM, not the grace)", elapsed)
	}
}

func TestExecTimeoutKillsWholeProcessGroup(t *testing.T) {
	result, err := (Cmd{Timeout: 300 * time.Millisecond, KillGrace: time.Second}).Exec(context.Background(),
		"sh", "-c", "sleep 100 & echo $!; wait")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	pid := firstLinePID(t, result.Stdout)
	if !processGone(t, pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("background sleep (pid %d) outlived the timeout", pid)
	}
}

func TestExecTimeoutKeepsOutputWrittenOnSIGTERM(t *testing.T) {
	start := time.Now()
	result, err := (Cmd{Timeout: 200 * time.Millisecond, KillGrace: 5 * time.Second}).Exec(context.Background(),
		"sh", "-c", `trap 'echo final-state; exit 143' TERM; sleep 100 & wait`)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if !strings.Contains(result.Stdout, "final-state") {
		t.Errorf("stdout = %q, want the line the SIGTERM handler printed", result.Stdout)
	}
	if result.ExitCode != 143 {
		t.Errorf("exit code = %d, want 143", result.ExitCode)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %s: the handler exited on SIGTERM, Exec should not wait out the 5s grace", elapsed)
	}
}

func TestExecTimeoutIsAnErrorEvenWhenCommandExitsZero(t *testing.T) {
	_, err := (Cmd{Timeout: 200 * time.Millisecond, KillGrace: 2 * time.Second}).Exec(context.Background(),
		"sh", "-c", `trap 'exit 0' TERM; sleep 100 & wait`)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout despite exit 0", err)
	}
}

func TestExecTimeoutSIGKILLsAfterGraceWhenSIGTERMIgnored(t *testing.T) {
	start := time.Now()
	// An ignored signal stays ignored across exec, so sleep ignores TERM too.
	_, err := (Cmd{Timeout: 100 * time.Millisecond, KillGrace: 400 * time.Millisecond}).Exec(context.Background(),
		"sh", "-c", `trap '' TERM; sleep 100`)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	elapsed := time.Since(start)
	if elapsed < 450*time.Millisecond {
		t.Errorf("took %s: the forced kill came before the grace ran out", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %s: SIGKILL should end it right after the 400ms grace", elapsed)
	}
}

func TestExecTimeoutZeroGraceKillsImmediately(t *testing.T) {
	start := time.Now()
	_, err := (Cmd{Timeout: 100 * time.Millisecond}).Exec(context.Background(),
		"sh", "-c", `trap '' TERM; sleep 100`)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %s, want an immediate SIGKILL with KillGrace 0", elapsed)
	}
}

// A descendant that left the process group keeps the output pipe open after
// the group is killed; WaitDelay must still let Exec return.
func TestExecTimeoutReturnsWhenEscapedDescendantHoldsPipe(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl not available to call setsid()")
	}
	old := pipeDrainDelay
	pipeDrainDelay = 300 * time.Millisecond
	defer func() { pipeDrainDelay = old }()

	start := time.Now()
	result, err := (Cmd{Timeout: 200 * time.Millisecond, KillGrace: 100 * time.Millisecond}).Exec(context.Background(),
		"sh", "-c", `perl -e 'use POSIX; setsid(); $|=1; print "$$\n"; sleep 100' & wait`)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %s: Exec blocked on a pipe held by an escaped descendant", elapsed)
	}
	if pid := firstLinePID(t, result.Stdout); pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func TestExecWithoutTimeoutIsUnchanged(t *testing.T) {
	result, err := (Cmd{}).Exec(context.Background(), "sh", "-c", "echo hi; exit 3")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want an *exec.ExitError", err)
	}
	if result.Stdout != "hi\n" || result.ExitCode != 3 {
		t.Errorf("result = %+v", result)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Minute:        "30m",
		90 * time.Second:        "90s",
		2 * time.Hour:           "2h",
		250 * time.Millisecond:  "250ms",
		1500 * time.Millisecond: "1.5s",
	}
	for d, want := range cases {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
