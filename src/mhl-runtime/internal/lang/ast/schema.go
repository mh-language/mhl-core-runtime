package ast

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alecthomas/participle/v2/lexer"
)

// Schema declares a JSON Schema file a program hands to an agent:
//
//	schema Brief from "schemas/brief.schema.json"
//
// Used as a value, Brief is `{content: string, path: string}` — content is
// the file's JSON text (what an engine taking the schema inline needs, e.g.
// `claude --json-schema`), path its absolute location (what an engine
// taking a file needs, e.g. `codex --output-schema`, which a subprocess
// resolves against its own working directory, not the .mh file's). Source
// is relative to the declaring file, like `prompt ... from`.
//
// Content and Path are empty until import resolution calls Load
// (internal/engine/interpreter/imports.go, internal/lang/lint/imports.go).
// Path is excluded from DefinitionDigest: it depends on where the checkout
// lives, and moving a project must not invalidate its checkpoints.
type Schema struct {
	Pos     lexer.Position
	Name    string `parser:"'schema' @Ident"`
	Source  string `parser:"'from' @String"`
	Content string
	Path    string `parser:"" digest:"-"`
}

// MaxSchemaSourceSize bounds a schema file, like a skill's source.
const MaxSchemaSourceSize = 1 << 20

// Load reads Source relative to dir into Content and Path, rejecting a file
// that is not a JSON object.
func (s *Schema) Load(dir string) error {
	full, err := filepath.Abs(filepath.Join(dir, s.Source))
	if err != nil {
		return err
	}
	src, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	if len(src) > MaxSchemaSourceSize {
		return fmt.Errorf("%d bytes exceeds the %d byte limit", len(src), MaxSchemaSourceSize)
	}
	var v any
	if err := json.Unmarshal(src, &v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if _, ok := v.(map[string]any); !ok {
		return fmt.Errorf("a JSON Schema must be a JSON object")
	}
	s.Content = string(src)
	s.Path = full
	return nil
}

// Value is the schema as an mhl value — what the declared name evaluates to.
func (s *Schema) Value() map[string]any {
	return map[string]any{"content": s.Content, "path": s.Path}
}
