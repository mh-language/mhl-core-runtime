//go:build windows

package tools

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConfigureProcessGroupHidesConsoleWindow(t *testing.T) {
	command := exec.Command("cmd.exe", "/c", "exit", "0")
	configureProcessGroup(command)
	if command.SysProcAttr == nil {
		t.Fatal("configureProcessGroup left SysProcAttr nil")
	}
	if !command.SysProcAttr.HideWindow {
		t.Error("configureProcessGroup did not set HideWindow")
	}
	want := uint32(syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow)
	if command.SysProcAttr.CreationFlags&want != want {
		t.Errorf("CreationFlags = %#x, want flags %#x", command.SysProcAttr.CreationFlags, want)
	}
}

func TestCheckCommandLineLengthAcceptsOrdinaryCommand(t *testing.T) {
	if err := checkCommandLineLength("claude", []string{"-p", "short prompt", "--output-format", "json"}); err != nil {
		t.Fatalf("unexpected error for a short command line: %v", err)
	}
}

func TestCheckCommandLineLengthRejectsOversizedCommand(t *testing.T) {
	hugePrompt := strings.Repeat("a", maxCommandLineLength)
	err := checkCommandLineLength("claude", []string{"-p", hugePrompt})
	if err == nil {
		t.Fatal("expected an error for a command line over the Windows limit, got nil")
	}
	if !strings.Contains(err.Error(), "stdin") {
		t.Errorf("error %q does not point the caller toward `stdin:`", err.Error())
	}
}

// Acceptance 1/2 on Windows: a timed-out command tree is terminated through
// its Job Object. The inner `ping` is a grandchild (cmd.exe → cmd.exe →
// ping) that Process.Kill alone would orphan, still holding the output pipe
// and keeping Exec waiting; with the job it dies with the tree.
func TestExecTimeoutTerminatesProcessTree(t *testing.T) {
	start := time.Now()
	_, err := (Cmd{Timeout: 300 * time.Millisecond, KillGrace: 5 * time.Second}).Exec(context.Background(),
		"cmd.exe", "/c", "cmd.exe /c ping -n 30 127.0.0.1")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	// No polite request on Windows: the grace is skipped, and the pipe
	// closes as soon as the job is terminated.
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %s: the process tree was not terminated", elapsed)
	}
}
