package nativeops

import (
	"bytes"
	"fmt"
	"io"
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

// appendWriter is an io.WriteCloser that appends a subprocess's output to a
// file, opening (and creating any missing parent directories for) that file
// lazily on the first non-empty Write rather than at construction — the same
// "created on first write" behavior Append has, extended to a stream of
// writes instead of one already-complete string. A caller that never writes
// to it, or writes only empty chunks, never touches the filesystem, and
// Close is a safe no-op in that case too.
//
// Writes are re-chunked to line boundaries: each complete line (with its
// trailing "\n") is flushed as one O_APPEND syscall, and any bytes past the
// last "\n" are held in buf until the newline that finishes them arrives (or
// until Close). Because a single O_APPEND write is atomic for a regular file,
// this keeps two runs that share one `log:` path from splicing a half-line of
// one run's output into the middle of the other's — their lines interleave,
// but no line is ever torn. A newline-less tail is flushed whole by Close.
type appendWriter struct {
	path string
	f    *os.File
	buf  []byte
}

func (w *appendWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.f == nil {
		if dir := filepath.Dir(w.path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return 0, fmt.Errorf("fs.append %q: creating %s: %w", w.path, dir, err)
			}
		}
		f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return 0, fmt.Errorf("fs.append %q: %w", w.path, err)
		}
		w.f = f
	}
	w.buf = append(w.buf, p...)
	for {
		nl := bytes.IndexByte(w.buf, '\n')
		if nl == -1 {
			break
		}
		if _, err := w.f.Write(w.buf[:nl+1]); err != nil {
			return len(p), fmt.Errorf("fs.append %q: %w", w.path, err)
		}
		w.buf = w.buf[nl+1:]
	}
	// Keep buf from pinning the whole backing array once it's been consumed
	// down to a short remainder.
	if len(w.buf) == 0 {
		w.buf = nil
	}
	return len(p), nil
}

func (w *appendWriter) Close() error {
	if w.f == nil {
		return nil
	}
	var flushErr error
	if len(w.buf) > 0 {
		_, flushErr = w.f.Write(w.buf)
		w.buf = nil
	}
	closeErr := w.f.Close()
	if flushErr != nil {
		return fmt.Errorf("fs.append %q: %w", w.path, flushErr)
	}
	return closeErr
}

// AppendWriter returns an io.WriteCloser that streams a subprocess's output
// to path as it arrives instead of requiring the full content up front like
// Append does — for a caller (e.g. an agent's `log:` property) that wants to
// persist output incrementally rather than buffering the whole thing in
// memory until the process exits. Output is flushed one line at a time so
// concurrent writers sharing a path never tear each other's lines (see
// appendWriter). The caller is responsible for calling Close once it's done
// writing, which also flushes any newline-less trailing bytes.
func AppendWriter(path string) io.WriteCloser {
	return &appendWriter{path: path}
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
