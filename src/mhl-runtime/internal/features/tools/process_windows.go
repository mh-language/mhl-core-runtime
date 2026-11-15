//go:build windows

package tools

import (
	"fmt"
	"os"
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

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
)

// Access rights AssignProcessToJobObject requires on the process handle.
const (
	processTerminate = 0x0001
	processSetQuota  = 0x0100
)

// processTree is a started command plus every process it spawns: the command
// is assigned to a fresh Job Object right after Start, and descendants
// inherit the job, so TerminateJobObject ends the whole tree — what
// Process.Kill alone (the direct child only) can't. A process spawned in the
// instant between Start and the assignment escapes the job; if the job can't
// be created or assigned at all, kill falls back to Process.Kill.
//
// The job is created without JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so closing
// it in release never kills anything — a command that finished normally and
// left a background process behind keeps the historical behavior.
type processTree struct {
	process *os.Process
	job     syscall.Handle
}

func attachProcessTree(command *exec.Cmd) *processTree {
	t := &processTree{process: command.Process}
	job, _, _ := procCreateJobObjectW.Call(0, 0)
	if job == 0 {
		return t
	}
	proc, err := syscall.OpenProcess(processTerminate|processSetQuota, false, uint32(command.Process.Pid))
	if err != nil {
		_ = syscall.CloseHandle(syscall.Handle(job))
		return t
	}
	defer syscall.CloseHandle(proc)
	if ok, _, _ := procAssignProcessToJobObject.Call(job, uintptr(proc)); ok == 0 {
		_ = syscall.CloseHandle(syscall.Handle(job))
		return t
	}
	t.job = syscall.Handle(job)
	return t
}

// terminate reports false: a console-less child (createNoWindow) can't
// receive CTRL_BREAK, and Windows has no other polite request, so a timed-out
// command goes straight to kill.
func (t *processTree) terminate() bool { return false }

// kill terminates the whole job (exit code 1), or just the direct child when
// no job could be attached.
func (t *processTree) kill() {
	if t.job != 0 {
		if ok, _, _ := procTerminateJobObject.Call(uintptr(t.job), 1); ok != 0 {
			return
		}
	}
	_ = t.process.Kill()
}

func (t *processTree) release() {
	if t.job != 0 {
		_ = syscall.CloseHandle(t.job)
		t.job = 0
	}
}
