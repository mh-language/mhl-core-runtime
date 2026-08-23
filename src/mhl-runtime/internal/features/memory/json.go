package memory

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
)

// JSONStore is a key-value store persisted as a single JSON object file.
// Unlike KVStore, its data lives on disk and survives across separate `mhl
// run` invocations: the object at path is loaded once (lazily, on first
// use) and rewritten in full on every Set. Data is cached per path so
// multiple memory declarations pointing at the same path correctly share
// one underlying file. Values are arbitrary JSON-compatible data (string,
// float64, bool, []any, map[string]any).
type JSONStore struct {
	mu     sync.Mutex
	caches map[string]map[string]any
}

// NewJSONStore returns a store with an empty cache.
func NewJSONStore() *JSONStore {
	return &JSONStore{caches: make(map[string]map[string]any)}
}

// Set stores value under key in the JSON object at path, creating the file
// (and any missing parent directories) if needed, and rewrites the whole
// object to disk immediately.
func (s *JSONStore) Set(path, key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load(path)
	if err != nil {
		return err
	}
	data[key] = value
	return writeJSON(path, data)
}

// SetAll merges values into the JSON object at path — one key per entry,
// same overwrite semantics as Set — and rewrites the whole object to disk
// immediately. It backs the `mem.set({...})` bulk-assign form.
func (s *JSONStore) SetAll(path string, values map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load(path)
	if err != nil {
		return err
	}
	maps.Copy(data, values)
	return writeJSON(path, data)
}

// Get returns the value stored under key in the JSON object at path, or def
// if the key was never set. key may navigate into a structured
// (array/object) value with "::"-separated segments, e.g. "cfg::retries" or
// "tags::0" to index into an array — see resolvePath. A key with no "::"
// behaves exactly as before.
func (s *JSONStore) Get(path, key string, def any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load(path)
	if err != nil {
		return nil, err
	}
	base, navPath := splitPathKey(key)
	v, ok := data[base]
	if !ok {
		return def, nil
	}
	if len(navPath) == 0 {
		return v, nil
	}
	resolved, ok := resolvePath(v, navPath)
	if !ok {
		return def, nil
	}
	return resolved, nil
}

// Remove deletes the value stored under key in the JSON object at path,
// rewriting the whole object to disk immediately, and reports whether the
// key existed. key may navigate into a structured (array/object) value with
// "::"-separated segments, e.g. "cfg::retries", to delete a nested field
// instead of a top-level one — see removePath. A key with no "::" deletes a
// top-level key, same as before nested removal existed.
func (s *JSONStore) Remove(path, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load(path)
	if err != nil {
		return false, err
	}
	base, navPath := splitPathKey(key)
	if len(navPath) == 0 {
		if _, ok := data[base]; !ok {
			return false, nil
		}
		delete(data, base)
	} else {
		root, ok := data[base]
		if !ok {
			return false, nil
		}
		updated, removed := removePath(root, navPath)
		if !removed {
			return false, nil
		}
		data[base] = updated
	}
	if err := writeJSON(path, data); err != nil {
		return false, err
	}
	return true, nil
}

// load returns the cached data for path, reading and parsing the file on
// first access. A missing file starts as an empty object; a malformed
// existing file is a hard error.
func (s *JSONStore) load(path string) (map[string]any, error) {
	if data, ok := s.caches[path]; ok {
		return data, nil
	}
	data := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("memory: parsing %s: %w", path, err)
		}
	case os.IsNotExist(err):
		// starts empty
	default:
		return nil, fmt.Errorf("memory: reading %s: %w", path, err)
	}
	s.caches[path] = data
	return data, nil
}

func writeJSON(path string, data map[string]any) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("memory: creating %s: %w", dir, err)
		}
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("memory: writing %s: %w", path, err)
	}
	return nil
}
