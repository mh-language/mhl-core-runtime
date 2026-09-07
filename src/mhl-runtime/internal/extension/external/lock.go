package external

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Lock is the parsed .mhl/extensions.lock. It is the project's explicit
// allow-list: an extension present on disk but absent here does not load.
// Version 1 pins only the executable; version 2 additionally pins the whole
// materialized package tree.
type Lock struct {
	LockfileVersion int                  `json:"lockfile_version,omitempty"`
	Extensions      map[string]LockEntry `json:"extensions"`
}

// LockEntry pins one extension. Version and SHA256 are always set. PackageSHA256
// is required by lockfile version 2 and covers every regular file in the
// materialized package. Source and Commit are recorded only for an extension installed from a git remote
// (`mhl extension install <url>[//<subdir>][#<ref>]`): Source is the spec as
// given, Commit is the 40-hex commit it resolved to — together they make a
// git install auditable and reproducible. A local-directory install leaves
// both empty, and an older lock without them still loads unchanged.
type LockEntry struct {
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
	PackageSHA256 string `json:"package_sha256,omitempty"`
	Source        string `json:"source,omitempty"`
	Commit        string `json:"commit,omitempty"`
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
	if l.LockfileVersion > 2 {
		return nil, fmt.Errorf("%s: unsupported lockfile version %d", path, l.LockfileVersion)
	}
	if l.LockfileVersion == 2 {
		for id, entry := range l.Extensions {
			if entry.PackageSHA256 == "" {
				return nil, fmt.Errorf("%s: extension %q has no package_sha256", path, id)
			}
		}
	}
	return &l, nil
}

// Save writes the lock to path, pretty-printed and newline-terminated,
// creating the parent directory if needed.
func (l *Lock) Save(path string) error {
	l.NormalizeVersion()
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

// HashPackage returns the SHA-256 of a materialized extension package. Its
// canonical input is each regular file's slash-separated relative path,
// permission bits, and bytes in lexical path order. Links and special files
// are rejected so a package's digest cannot depend on content outside its root.
func HashPackage(root string) (string, error) {
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package contains symlink %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("package contains non-regular file %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return "", err
	}

	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, rel+"\x00"+strconv.FormatUint(uint64(info.Mode().Perm()), 8)+"\x00"+strconv.FormatInt(info.Size(), 10)+"\x00"); err != nil {
			return "", err
		}
		if _, err := h.Write(data); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, "\x00"); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsV2 reports whether lock entries must include complete package hashes.
func (l *Lock) IsV2() bool { return l.LockfileVersion >= 2 }

// NormalizeVersion upgrades a lock being rewritten while preserving readable
// version 1 input until a mutating command supplies complete package hashes.
func (l *Lock) NormalizeVersion() {
	if l.LockfileVersion == 0 {
		l.LockfileVersion = 1
	}
	if l.LockfileVersion > 2 {
		return
	}
	for _, entry := range l.Extensions {
		if strings.TrimSpace(entry.PackageSHA256) == "" {
			return
		}
	}
	l.LockfileVersion = 2
}
