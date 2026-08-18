//go:build windows

package tools

import "os/exec"

// Windows uses the process handle as the cancellation boundary. The helper is
// isolated here so the command runner remains portable without shelling out.
func configureProcessGroup(command *exec.Cmd) {}
func killProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
