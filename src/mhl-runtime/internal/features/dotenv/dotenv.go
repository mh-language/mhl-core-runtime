// Package dotenv reads `.env` files — KEY=VALUE lines — into the process
// environment, so `mhl run`/`mhl test` see API keys and settings kept next to
// a project instead of exported in a shell profile.
//
// The format is the common subset every dotenv tool agrees on:
//
//	# a comment
//	KEY=value             # unquoted: trimmed, " #..." starts a comment
//	export KEY=value      # a leading `export` is ignored
//	KEY="a\nb \"q\""      # double quotes: \n \r \t \" \\ are escapes
//	KEY='literal $HOME'   # single quotes: taken as written
//	KEY=                  # set, to the empty string
//
// There is no variable expansion and no multi-line value. A variable already
// in the environment is never overridden: the shell (or the caller) wins.
package dotenv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// DefaultFile is the file `mhl run`/`mhl test` load from the working
// directory when no --env-file is given.
const DefaultFile = ".env"

// Var is one KEY=VALUE pair, in file order.
type Var struct {
	Key   string
	Value string
}

// Parse reads dotenv content. name only labels errors ("name:line: ...").
func Parse(r io.Reader, name string) ([]Var, error) {
	var vars []Var
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if n == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		v, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, n, err)
		}
		vars = append(vars, v)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return vars, nil
}

func parseLine(line string) (Var, error) {
	if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		line = strings.TrimSpace(rest)
	}
	key, raw, ok := strings.Cut(line, "=")
	if !ok {
		return Var{}, fmt.Errorf("expected KEY=VALUE")
	}
	key = strings.TrimSpace(key)
	if !validKey(key) {
		return Var{}, fmt.Errorf("invalid variable name %q", key)
	}
	value, err := parseValue(strings.TrimSpace(raw))
	if err != nil {
		return Var{}, fmt.Errorf("%s: %w", key, err)
	}
	return Var{Key: key, Value: value}, nil
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i, c := range k {
		switch {
		case c == '_', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func parseValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '\'':
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 {
			return "", fmt.Errorf("unterminated single quote")
		}
		return raw[1 : 1+end], trailing(raw[2+end:])
	case '"':
		var b strings.Builder
		for i := 1; i < len(raw); i++ {
			c := raw[i]
			switch {
			case c == '"':
				return b.String(), trailing(raw[i+1:])
			case c == '\\' && i+1 < len(raw):
				i++
				switch raw[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				case '"', '\\':
					b.WriteByte(raw[i])
				default:
					b.WriteByte('\\')
					b.WriteByte(raw[i])
				}
			default:
				b.WriteByte(c)
			}
		}
		return "", fmt.Errorf("unterminated double quote")
	}
	// Unquoted: a '#' after whitespace starts a comment.
	for i := 1; i < len(raw); i++ {
		if raw[i] == '#' && (raw[i-1] == ' ' || raw[i-1] == '\t') {
			raw = raw[:i]
			break
		}
	}
	return strings.TrimSpace(raw), nil
}

// trailing accepts only whitespace or a comment after a quoted value.
func trailing(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, "#") {
		return nil
	}
	return fmt.Errorf("unexpected %q after the quoted value", rest)
}

// Load reads files in order (a later file overrides an earlier one) and sets
// every variable the environment does not already have. It returns the keys
// it set. A missing file is an error unless optional is true.
func Load(files []string, optional bool) ([]string, error) {
	merged := map[string]string{}
	var order []string
	for _, f := range files {
		data, err := os.Open(f)
		if err != nil {
			if optional && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("env file: %w", err)
		}
		vars, err := Parse(data, f)
		data.Close()
		if err != nil {
			return nil, fmt.Errorf("env file %w", err)
		}
		for _, v := range vars {
			if _, seen := merged[v.Key]; !seen {
				order = append(order, v.Key)
			}
			merged[v.Key] = v.Value
		}
	}
	var set []string
	for _, k := range order {
		if _, exists := os.LookupEnv(k); exists {
			continue
		}
		if err := os.Setenv(k, merged[k]); err != nil {
			return set, fmt.Errorf("env file: setting %s: %w", k, err)
		}
		set = append(set, k)
	}
	return set, nil
}
