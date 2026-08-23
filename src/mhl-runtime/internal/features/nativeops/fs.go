package nativeops

import (
	"fmt"
	"os"
	"path/filepath"
)

// Read returns path's full contents as a string.
func Read(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("fs.read %q: %w", path, err)
	}
	return string(raw), nil
}

// Exists reports whether path names an existing file or directory. Any
// stat error other than "not found" (e.g. a permission error) is returned
// rather than silently reported as false, since that distinction matters to
// a caller deciding whether it's safe to proceed.
func Exists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("fs.exists %q: %w", path, err)
	}
	return true, nil
}

// Write writes content to path, creating any missing parent directories
// (same convention as internal/memory/json.go's writeJSON). Returns true on
// success — there's nothing more meaningful to hand back for a write.
func Write(path, content string) (bool, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, fmt.Errorf("fs.write %q: creating %s: %w", path, dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("fs.write %q: %w", path, err)
	}
	return true, nil
}

// Append adds content to the end of path, creating both the file and any
// missing parent directories if they don't exist yet (same convention as
// Write). Unlike a Read-modify-Write round trip, this never holds the
// file's full prior content in memory and never races a concurrent
// appender into clobbering the other's write — each write is a single
// O_APPEND syscall, whose atomicity for a write this size the OS itself
// guarantees.
func Append(path, content string) (bool, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, fmt.Errorf("fs.append %q: creating %s: %w", path, dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Errorf("fs.append %q: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return false, fmt.Errorf("fs.append %q: %w", path, err)
	}
	return true, nil
}
