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
