package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NewSessionID returns a fresh, opaque id for one pipeline execution: 16
// crypto/rand bytes, hex-encoded. Deliberately not an RFC 4122 UUID library
// — this repo keeps a single external dependency (participle), and 16 random
// bytes serve the same "practically unique, opaque filename component, never
// parsed or compared structurally" purpose. Each `mhl run` gets its own id
// so two concurrent runs of the same pipeline never share a state file (see
// Store.Session); newInstanceID (loop.go) delegates here.
func NewSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read failing means no entropy source at all — fall
		// back to a timestamp rather than panic, so a pathological
		// environment still gets *an* id, just a weaker one.
		return "t" + hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}

// latestPath is the pointer file, directly under the base state dir, that
// records the most recent session id for a pipeline. A bare `mhl run
// --resume` reads it to learn which session directory to continue, since the
// per-session layout means the pipeline name alone no longer identifies one
// checkpoint file.
func latestPath(baseDir, pipeline string) string {
	return filepath.Join(baseDir, pipeline+".latest")
}

// writeLatest atomically records id as pipeline's most recent session.
func writeLatest(baseDir, pipeline, id string) error {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("runtime: creating state dir: %w", err)
	}
	tmp := latestPath(baseDir, pipeline) + ".tmp"
	if err := os.WriteFile(tmp, []byte(id+"\n"), 0o644); err != nil {
		return fmt.Errorf("runtime: writing latest pointer: %w", err)
	}
	if err := os.Rename(tmp, latestPath(baseDir, pipeline)); err != nil {
		return fmt.Errorf("runtime: committing latest pointer: %w", err)
	}
	return nil
}

// readLatest returns the recorded most-recent session id for pipeline, or
// ok=false when no pointer file exists yet.
func readLatest(baseDir, pipeline string) (string, bool) {
	data, err := os.ReadFile(latestPath(baseDir, pipeline))
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", false
	}
	return id, true
}

// ResolveSession decides which session id a `mhl run` should use for
// pipeline, given the raw --session flag value (empty when unset) and
// whether --resume was passed. store must be an unscoped Store (its dir is
// the base state dir).
//
//   - --session <id> always wins, on a fresh run and a resume alike.
//   - a bare --resume follows the .latest pointer; failing that it falls
//     back to a legacy top-level <base>/<pipeline>.json checkpoint written by
//     an older mhl (returned as an empty id, which keeps the Store unscoped
//     so Runner.Run's own Load finds it); failing that it starts a fresh
//     session (nothing to resume ⇒ run from the start, unchanged).
//   - a fresh run always gets a brand-new id.
func ResolveSession(store *Store, pipeline, sessionFlag string, resume bool) string {
	if sessionFlag != "" {
		return sessionFlag
	}
	if resume {
		if id, ok := readLatest(store.base, pipeline); ok {
			return id
		}
		if _, err := os.Stat(filepath.Join(store.base, pipeline+".json")); err == nil {
			return ""
		}
	}
	return NewSessionID()
}

// PriorVars returns the variable state a pipeline's `context:` element
// should expose as context.vars, per its configured source: "latest"
// follows the .latest pointer to the pipeline's most recent session,
// "session:<id>" pins an explicit one. Within that session it prefers
// result.json (a run that completed) and falls back to a still-present
// checkpoint's variables (a run that crashed or is mid-flight); nil when
// nothing is found. store must be unscoped (its dir is the base state dir).
func PriorVars(store *Store, pipeline, source string) (map[string]any, error) {
	id := ""
	if strings.HasPrefix(source, "session:") {
		id = strings.TrimSpace(strings.TrimPrefix(source, "session:"))
	} else if got, ok := readLatest(store.base, pipeline); ok {
		id = got
	}
	if id == "" {
		return nil, nil
	}
	dir := filepath.Join(store.base, id)
	if data, err := os.ReadFile(filepath.Join(dir, "result.json")); err == nil {
		var v map[string]any
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("runtime: decoding %s/result.json: %w", id, err)
		}
		return v, nil
	}
	if data, err := os.ReadFile(filepath.Join(dir, pipeline+".json")); err == nil {
		var cp Checkpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			return nil, fmt.Errorf("runtime: decoding %s checkpoint: %w", id, err)
		}
		return cp.Variables, nil
	}
	return nil, nil
}

// sessionPruneGrace is how old a session directory must be, by mtime, before
// PruneExpired will consider removing it — a window long enough that a run
// paused overnight for a `--resume` in the morning is never swept.
const sessionPruneGrace = 7 * 24 * time.Hour

// sessionPruneScanCap bounds how many session directories a single
// PruneExpired call inspects, so a pathologically large .mhl/state never
// turns the best-effort sweep at the top of a run into a noticeable stall.
const sessionPruneScanCap = 500

// PruneExpired best-effort removes session directories under baseDir whose
// checkpoint is missing or past its TTL and whose directory mtime is older
// than sessionPruneGrace. It is called opportunistically at the start of a
// run; every error is the caller's to ignore.
func PruneExpired(baseDir string) error {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return err
	}
	now := time.Now()
	scanned := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if scanned >= sessionPruneScanCap {
			break
		}
		scanned++
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < sessionPruneGrace {
			continue
		}
		dir := filepath.Join(baseDir, e.Name())
		if sessionDirIsStale(dir, now) {
			_ = os.RemoveAll(dir)
		}
	}
	return nil
}

// sessionDirIsStale reports whether dir holds no still-live checkpoint — no
// <pipeline>.json (or loop-<pipeline>.json) whose contents parse and are not
// yet expired. A directory left with only a result.json, or nothing, counts
// as stale once it is past the grace period PruneExpired already checked.
func sessionDirIsStale(dir string, now time.Time) bool {
	files, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || !strings.HasSuffix(name, ".json") || name == "result.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var cp Checkpoint
		if json.Unmarshal(data, &cp) == nil && !cp.Expired(now) {
			return false
		}
	}
	return true
}
