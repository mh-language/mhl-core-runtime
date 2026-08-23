// Package memory implements the storage backends declared by `memory`
// blocks (language-design.md §6 "Gerenciamento de Memória"): an in-memory
// key-value store (`type: "kv"`, `store: "memory"`), a disk-persisted
// key-value store (`type: "json"`), an append-only text log file
// (`type: "append_log"`), and an append-only structured JSON log
// (`type: "jsonl"`).
package memory

import "sync"

// KVStore is an in-memory key-value store scoped to a single `mhl run`
// invocation. Keys are namespaced per memory declaration name, so multiple
// `memory` blocks in the same program never collide. Values are arbitrary
// JSON-compatible data (string, float64, bool, []any, map[string]any) —
// whatever a .mh literal expression evaluates to.
type KVStore struct {
	mu   sync.Mutex
	data map[string]map[string]any
}

// NewKVStore returns an empty store.
func NewKVStore() *KVStore {
	return &KVStore{data: make(map[string]map[string]any)}
}

// Set stores value under key, scoped to memoryName.
func (s *KVStore) Set(memoryName, key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[memoryName] == nil {
		s.data[memoryName] = make(map[string]any)
	}
	s.data[memoryName][key] = value
}

// Get returns the value stored under key for memoryName, or def if it was
// never set. key may navigate into a structured (array/object) value with
// "::"-separated segments, e.g. "cfg::retries" or "tags::0" to index into an
// array — see resolvePath. A key with no "::" behaves exactly as before.
func (s *KVStore) Get(memoryName, key string, def any) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	base, path := splitPathKey(key)
	v, ok := s.data[memoryName][base]
	if !ok {
		return def
	}
	if len(path) == 0 {
		return v
	}
	resolved, ok := resolvePath(v, path)
	if !ok {
		return def
	}
	return resolved
}
