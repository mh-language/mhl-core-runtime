package external

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// LockPath is the project-relative path to the extension lock file.
const LockPath = ".mhl/extensions.lock"

// Problem is one thing wrong with an installed/locked extension — surfaced by
// `mhl extension doctor` and by Discover so a bad extension is reported, not
// silently skipped.
type Problem struct {
	ID      string
	Message string
}

// Set is the collection of external extensions resolved for one runtime
// invocation. It owns the extension processes; CloseAll shuts them down.
type Set struct {
	exts     []*External
	problems []Problem
}

// Extensions returns the resolved extensions as the generic interface, ready
// to register into a runtime registry.
func (s *Set) Extensions() []extension.Extension {
	out := make([]extension.Extension, len(s.exts))
	for i, e := range s.exts {
		out[i] = e
	}
	return out
}

// Problems lists what could not be loaded.
func (s *Set) Problems() []Problem { return s.problems }

// CloseAll gracefully shuts down every extension process. Safe to call once,
// at the end of a run.
func (s *Set) CloseAll() {
	for _, e := range s.exts {
		_ = e.Close()
	}
}

// searchDirs is where an extension id is looked up, in order: the project's
// own vendored dir first, then the user-global install dir.
func searchDirs(projectDir string) []string {
	dirs := []string{filepath.Join(projectDir, ".mhl", "extensions")}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".mhl", "extensions"))
	}
	return dirs
}

// Status is one lock entry's resolution outcome — for `mhl extension
// list`/`doctor`, which never spawn a process.
type Status struct {
	ID       string
	Version  string
	Kinds    []string
	Manifest string
	OK       bool
	Problem  string
}

// Discover reads projectDir/.mhl/extensions.lock and returns a Set with one
// External per locked extension whose manifest is valid and whose executable
// hashes to the pinned sha256. An extension on disk but not in the lock is
// ignored (explicit allow-list). A locked extension that can't be resolved is
// recorded as a Problem, not an error — one bad entry never blocks the rest.
func Discover(projectDir string) (*Set, error) {
	lock, err := LoadLock(filepath.Join(projectDir, LockPath))
	if err != nil {
		return nil, err
	}

	set := &Set{}
	for id, entry := range lock.Extensions {
		m, prob := resolveLocked(projectDir, id, entry)
		if prob != "" {
			set.problems = append(set.problems, Problem{ID: id, Message: prob})
			continue
		}
		set.exts = append(set.exts, New(m))
	}
	return set, nil
}

// Inspect resolves every lock entry to a Status without building an External
// or starting anything — the read-only view `mhl extension list`/`doctor`
// print.
func Inspect(projectDir string) ([]Status, error) {
	lock, err := LoadLock(filepath.Join(projectDir, LockPath))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(lock.Extensions))
	for id := range lock.Extensions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]Status, 0, len(ids))
	for _, id := range ids {
		m, prob := resolveLocked(projectDir, id, lock.Extensions[id])
		st := Status{ID: id, OK: prob == "", Problem: prob}
		if m != nil {
			st.Version = m.Version
			st.Manifest = m.manifestPath
			for _, d := range m.Declares {
				st.Kinds = append(st.Kinds, d.Kind)
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// resolveLocked finds and fully validates the extension for one lock entry.
// It returns the loaded manifest (nil only when no manifest could be read)
// and an empty string on success, or a human-readable problem. It never
// spawns the process.
func resolveLocked(projectDir, id string, entry LockEntry) (*Manifest, string) {
	var manifestPath string
	for _, dir := range searchDirs(projectDir) {
		p := filepath.Join(dir, id, "extension.json")
		if _, err := os.Stat(p); err == nil {
			manifestPath = p
			break
		}
	}
	if manifestPath == "" {
		return nil, "locked but not installed (no extension.json in any search dir)"
	}

	m, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, err.Error()
	}
	if m.ID != id {
		return m, fmt.Sprintf("manifest id %q does not match lock key %q", m.ID, id)
	}
	if entry.Version != "" && m.Version != entry.Version {
		return m, fmt.Sprintf("installed version %q does not match locked %q", m.Version, entry.Version)
	}

	exe := m.ExecutablePath()
	info, err := os.Stat(exe)
	if err != nil {
		return m, "executable not found: " + exe
	}
	if info.Mode()&0o111 == 0 {
		return m, "executable is not executable: " + exe
	}
	sum, err := fileSHA256(exe)
	if err != nil {
		return m, "hashing executable: " + err.Error()
	}
	if entry.SHA256 == "" {
		return m, "lock entry has no sha256"
	}
	if sum != entry.SHA256 {
		return m, "executable sha256 does not match the lock (refusing to run a changed binary)"
	}
	return m, ""
}
