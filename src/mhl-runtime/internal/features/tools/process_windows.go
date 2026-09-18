//go:build windows

package tools

import (
	"fmt"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// maxCommandLineLength is CreateProcess's hard cap on lpCommandLine, in
// characters — 32,768 UTF-16 code units including the terminating null, so
// 32,767 of actual content. An oversized command line never reaches the
// Anthropic/OpenAI backend at all: CreateProcess itself refuses to start the
// child process, surfacing as Go's raw, backend-agnostic "The filename or
// extension is too long" (ERROR_FILENAME_EXCED_RANGE) with no indication the
// real cause is argv size. macOS/Linux have no equivalent of this — their
// execve ARG_MAX is on the order of 1-2MB — so this check only exists here.
const maxCommandLineLength = 32767

// checkCommandLineLength estimates the lpCommandLine string CreateProcess
// will actually receive for name+args — mirroring how Go's os/exec builds it
// on Windows (each argument, syscall.EscapeArg-escaped, joined by a single
// space) — and fails fast with a diagnostic naming the real cause instead of
// letting CreateProcess refuse the process and surface only
// ERROR_FILENAME_EXCED_RANGE. This is what an agent's large `${prompt}`/
// `${schema}` placeholder in `args:` runs into; routing that content through
// the agent's `stdin:` property instead keeps it out of argv entirely.
func checkCommandLineLength(name string, args []string) error {
	total := len(syscall.EscapeArg(name))
	for _, a := range args {
		total += 1 + len(syscall.EscapeArg(a))
	}
	if total <= maxCommandLineLength {
		return nil
	}
	return fmt.Errorf(
		"command line for %q is %d characters, over the Windows CreateProcess limit of %d — move large arguments (a prompt, a schema) out of `args:` and into the agent's `stdin:` property instead",
		name, total, maxCommandLineLength,
	)
}

// Windows uses the process handle as the cancellation boundary. The helper is
// isolated here so the command runner remains portable without shelling out.
func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow,
		HideWindow:    true,
	}
}
func killProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
