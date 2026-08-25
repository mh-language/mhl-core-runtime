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

// List returns the paths of dir's immediate entries — files and
// subdirectories, one level, not recursive — as filepath.Join(dir, name)
// each, so a caller can feed a result straight into fs.read/fs.write/
// fs.delete without reassembling the path itself. os.ReadDir already
// returns entries sorted by filename, so the result is deterministic across
// runs without an extra sort here.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("fs.list %q: %w", dir, err)
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = filepath.Join(dir, e.Name())
	}
	return out, nil
}

// Join combines parts into a single path using the OS-appropriate separator,
// exactly like filepath.Join — cleaning the result and dropping empty parts.
func Join(parts ...string) string {
	return filepath.Join(parts...)
}

// Delete removes path — a file or an empty directory. Unlike Exists, it
// does not treat "already gone" as success: os.Remove errors when path
// doesn't exist, and that error is returned as-is (not wrapped in the usual
// "fs.delete %q: %w" — the .mh caller already gets the exact path back from
// the underlying PathError), so a .mh script that only expects an already-
// created file to be there (e.g. clearing a stale plan file between runs)
// can tell "deleted" from "was never created" via try/catch rather than
// having both look like a silent no-op.
func Delete(path string) (bool, error) {
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}
