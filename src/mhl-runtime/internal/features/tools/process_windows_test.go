//go:build windows

package tools

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"
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
