// Package frontmatter reads the flat "key: value" metadata block that may
// open a `skill ... from` or `prompt ... from` source file — a minimal,
// dependency-free reader, not a general YAML parser: no nesting, no lists,
// no multi-line values. It exists because nothing in this repository
// depends on YAML today, and a skill's SKILL.md needs only two required
// scalar fields (name, description) plus whatever else an author adds.
package frontmatter

import (
	"fmt"
	"strconv"
	"strings"
)

// delimiter is the line that opens and closes a frontmatter block.
const delimiter = "---"

// Parse splits src into an optional leading frontmatter block and the
// remaining body. A frontmatter block is a line containing only "---",
// followed by zero or more "key: value" lines (blank lines allowed, no
// nesting), followed by a closing "---" line — all at the very start of
// src, ignoring leading blank lines. When src has no such block, fm is an
// empty, non-nil map and body is src with leading/trailing whitespace
// trimmed — the same trimming loadPromptSource has always applied, so a
// file with no frontmatter behaves exactly as it did before this package
// existed.
//
// A value is read as a JSON-ish scalar where possible (true/false/a
// quoted string/a number) and as a bare trimmed string otherwise. A block
// that's opened but never closed, or a non-blank line inside it that isn't
// "key: value", is an error.
func Parse(src string) (map[string]any, string, error) {
	lines := strings.Split(src, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || strings.TrimSpace(lines[start]) != delimiter {
		return map[string]any{}, strings.TrimSpace(src), nil
	}

	fm := map[string]any{}
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == delimiter {
			return fm, strings.TrimSpace(strings.Join(lines[i+1:], "\n")), nil
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, "", fmt.Errorf("frontmatter line %d: expected \"key: value\", got %q", i+1, line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, "", fmt.Errorf("frontmatter line %d: empty key", i+1)
		}
		fm[key] = parseScalar(strings.TrimSpace(value))
	}
	return nil, "", fmt.Errorf("frontmatter block opened at line %d is never closed with %q", start+1, delimiter)
}

// parseScalar reads a frontmatter value as a bool/number when it looks like
// one, a de-quoted string when wrapped in double quotes, or the trimmed raw
// text otherwise.
func parseScalar(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if unquoted, err := strconv.Unquote(v); err == nil {
			return unquoted
		}
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		return n
	}
	return v
}

// RequireKeys validates that fm has every key in keys, each a non-empty
// string — used by a caller (skill loading) for which some keys are
// mandatory, unlike `prompt ... from`, where frontmatter is always optional
// and freeform.
func RequireKeys(fm map[string]any, keys ...string) error {
	for _, k := range keys {
		v, ok := fm[k]
		if !ok {
			return fmt.Errorf("missing required frontmatter key %q", k)
		}
		s, ok := v.(string)
		if !ok || s == "" {
			return fmt.Errorf("frontmatter key %q must be a non-empty string", k)
		}
	}
	return nil
}
