//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// processTree is a started command's whole process group — the leader
// configureProcessGroup made it, plus every descendant that didn't leave the
// group (setsid). Signals go to the negative pid, i.e. the group.
type processTree struct{ pid int }

func attachProcessTree(command *exec.Cmd) *processTree {
	return &processTree{pid: command.Process.Pid}
}

// terminate asks the whole group to exit (SIGTERM), leaving it room to flush
// its final state. It reports true: Unix always has a polite request.
func (t *processTree) terminate() bool {
	_ = syscall.Kill(-t.pid, syscall.SIGTERM)
	return true
}

// kill forcibly ends every process still in the group (SIGKILL).
func (t *processTree) kill() {
	_ = syscall.Kill(-t.pid, syscall.SIGKILL)
}

func (t *processTree) release() {}

// checkCommandLineLength is a no-op outside Windows: execve's ARG_MAX on
// macOS/Linux is on the order of 1-2MB (getconf ARG_MAX), dozens of times
// CreateProcess's ~32,767-character cap, so it isn't a practical limit for
// anything mhl's agent blocks build. See process_windows.go's copy of this
// function for the real check.
func checkCommandLineLength(name string, args []string) error {
	return nil
}
