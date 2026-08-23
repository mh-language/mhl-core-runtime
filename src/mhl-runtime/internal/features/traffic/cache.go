package traffic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type cacheEntry struct {
	Value     any       `json:"value"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Cache stores exact response values until their configured expiry.
type Cache struct {
	Dir string

	mu      sync.Mutex
	entries map[string]cacheEntry
	now     func() time.Time
}

// NewCache creates an in-memory cache. A non-empty dir also persists entries
// as JSON files, using the same SHA-256 key as the in-memory store.
func NewCache(dir string) *Cache {
	return &Cache{Dir: dir, entries: map[string]cacheEntry{}, now: time.Now}
}

// RequestKey returns the deterministic SHA-256 key for an exact request.
func RequestKey(engine, prompt string, parameters any) string {
	payload, _ := json.Marshal(struct {
		Engine     string `json:"engine"`
		Prompt     string `json:"prompt"`
		Parameters any    `json:"parameters"`
	}{engine, prompt, parameters})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// Get returns a live cached value. Expired entries are treated as misses and
// removed, including entries loaded from disk.
func (c *Cache) Get(key string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok && c.Dir != "" {
		entry, ok = c.readLocked(key)
	}
	if !ok {
		return nil, false
	}
	if !entry.ExpiresAt.IsZero() && !c.now().Before(entry.ExpiresAt) {
		delete(c.entries, key)
		if c.Dir != "" {
			_ = os.Remove(c.path(key))
		}
		return nil, false
	}
	return entry.Value, true
}

// Set stores a value for ttl. Non-positive TTL is rejected by treating the
// entry as immediately expired.
func (c *Cache) Set(key string, value any, ttl time.Duration) error {
	return c.set(key, value, ttl)
}

// SetMCP stores an MCP result, limiting its lifetime to the server-reported
// ttlMs even when the agent cache TTL is longer.
func (c *Cache) SetMCP(key string, value any, agentTTL time.Duration, ttlMs int64) error {
	mcpTTL := time.Duration(ttlMs) * time.Millisecond
	if agentTTL <= 0 || mcpTTL < agentTTL {
		agentTTL = mcpTTL
	}
	return c.set(key, value, agentTTL)
}

func (c *Cache) set(key string, value any, ttl time.Duration) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]cacheEntry{}
	}
	entry := cacheEntry{Value: value, ExpiresAt: c.now().Add(ttl)}
	c.entries[key] = entry
	if c.Dir == "" {
		return nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path(key), data, 0o600)
}

func (c *Cache) path(key string) string { return filepath.Join(c.Dir, key+".json") }

func (c *Cache) readLocked(key string) (cacheEntry, bool) {
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	if json.Unmarshal(data, &entry) != nil {
		return cacheEntry{}, false
	}
	c.entries[key] = entry
	return entry, true
}
