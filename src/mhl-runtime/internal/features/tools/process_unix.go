//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}

// checkCommandLineLength is a no-op outside Windows: execve's ARG_MAX on
// macOS/Linux is on the order of 1-2MB (getconf ARG_MAX), dozens of times
// CreateProcess's ~32,767-character cap, so it isn't a practical limit for
// anything mhl's agent blocks build. See process_windows.go's copy of this
// function for the real check.
func checkCommandLineLength(name string, args []string) error {
	return nil
}
