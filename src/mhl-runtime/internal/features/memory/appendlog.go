package memory

import (
	"fmt"
	"os"
	"path/filepath"
)

// Append writes line as a new line to the file at path, creating the file
// (and any missing parent directories) if it doesn't exist yet.
func Append(path, line string) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("memory: creating %s: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("memory: opening %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("memory: writing %s: %w", path, err)
	}
	return nil
}
