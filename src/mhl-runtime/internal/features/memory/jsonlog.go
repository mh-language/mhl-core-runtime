package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type jsonlEntry struct {
	Value any    `json:"value"`
	TS    string `json:"ts"`
}

// AppendJSONL writes value as a structured JSON line — {"value": value,
// "ts": "<RFC3339>"} — to the file at path, creating the file (and any
// missing parent directories) if it doesn't exist yet. Mirrors Append, but
// each entry is JSON-encoded instead of raw text. value may be any
// JSON-compatible data (string, float64, bool, []any, map[string]any).
func AppendJSONL(path string, value any) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("memory: creating %s: %w", dir, err)
		}
	}
	line, err := json.Marshal(jsonlEntry{Value: value, TS: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return fmt.Errorf("memory: encoding entry: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("memory: opening %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("memory: writing %s: %w", path, err)
	}
	return nil
}
