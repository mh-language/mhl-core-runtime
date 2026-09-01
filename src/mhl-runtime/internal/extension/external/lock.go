package external

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Lock is the parsed .mhl/extensions.lock. It is the project's explicit
// allow-list: an extension present on disk but absent here does not load. The
// sha256 pins the exact executable a given version resolves to, so a swapped
// binary is refused.
type Lock struct {
	Extensions map[string]LockEntry `json:"extensions"`
}

// LockEntry pins one extension. Version and SHA256 are always set. Source and
// Commit are recorded only for an extension installed from a git remote
// (`mhl extension install <url>[//<subdir>][#<ref>]`): Source is the spec as
// given, Commit is the 40-hex commit it resolved to — together they make a
// git install auditable and reproducible. A local-directory install leaves
// both empty, and an older lock without them still loads unchanged.
type LockEntry struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Source  string `json:"source,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

// LoadLock reads path. A missing file is not an error — it means "this
// project uses no external extensions" and yields an empty Lock.
func LoadLock(path string) (*Lock, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{Extensions: map[string]LockEntry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var l Lock
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if l.Extensions == nil {
		l.Extensions = map[string]LockEntry{}
	}
	return &l, nil
}

// Save writes the lock to path, pretty-printed and newline-terminated,
// creating the parent directory if needed.
func (l *Lock) Save(path string) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// HashExecutable returns the lowercase hex sha256 of the manifest's
// executable — the value that goes in a lock entry's "sha256".
func HashExecutable(m *Manifest) (string, error) {
	return fileSHA256(m.ExecutablePath())
}

// fileSHA256 returns the lowercase hex sha256 of the file at path.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
