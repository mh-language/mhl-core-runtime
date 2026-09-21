package nativeops

import (
	"fmt"
	"os"
	"os/user"
	"runtime"
)

// User returns the OS account name running the mhl process (os/user.Current,
// built CGO_ENABLED=0 so this resolves without cgo on every release
// target). A lookup failure (e.g. no matching /etc/passwd entry in a
// minimal container) is returned as an error rather than papered over with
// "", the same fail-closed stance as fs.read on a missing file.
func User() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("os.user: %w", err)
	}
	return u.Username, nil
}

// HomeDir returns the current user's home directory (os.UserHomeDir).
func HomeDir() (string, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("os.home_dir: %w", err)
	}
	return dir, nil
}

// Hostname returns the host's name as reported by the kernel (os.Hostname).
func Hostname() (string, error) {
	name, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("os.hostname: %w", err)
	}
	return name, nil
}

// Platform returns the running binary's target OS — "darwin", "linux", or
// "windows" for the platforms this repo actually builds (runtime.GOOS).
func Platform() string {
	return runtime.GOOS
}

// Arch returns the running binary's target architecture, e.g. "amd64" or
// "arm64" (runtime.GOARCH).
func Arch() string {
	return runtime.GOARCH
}

// Cwd returns the interpreter process's current working directory
// (os.Getwd) — the directory relative paths in fs.*/dir.* resolve against.
func Cwd() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("os.cwd: %w", err)
	}
	return dir, nil
}

// Pid returns the mhl process's OS process id (os.Getpid). float64, the
// same numeric representation every other native op returns (time.diff,
// time.compare, ...).
func Pid() float64 {
	return float64(os.Getpid())
}
