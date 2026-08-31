// store-fs is a reference implementation of an mhl `store`-kind extension: the
// key/value backend `mhl serve mcp --http` uses for durable run and session
// state when the workflow directory declares `extension store <Name> { dir: ... }`.
//
// It speaks the newline-delimited JSON-RPC protocol on stdin/stdout
// (see sample/extensions/extension-protocol.md) and keeps each key as one
// JSON file mirrored under `dir` — so `run/<id>/checkpoint/<pipeline>` is a
// real path you can inspect. A production backend (DynamoDB, Redis) replaces
// the storage; the four operations and the wire shape are the whole contract.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	extID      = "dev.mhl.store-fs"
	extVersion = "0.1.0"
	apiVersion = "1"
)

type rpc struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

type callParams struct {
	Declaration struct {
		Props []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"props"`
	} `json:"declaration"`
	Operation string         `json:"operation"`
	NamedArgs map[string]any `json:"named_args"`
}

func main() {
	s := &store{}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 8<<20)
	out := json.NewEncoder(os.Stdout)

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var msg rpc
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			_ = out.Encode(rpc{ID: msg.ID, Result: map[string]any{
				"api_version": apiVersion,
				"extension":   map[string]string{"id": extID, "version": extVersion},
			}})
		case "call":
			_ = out.Encode(s.handleCall(msg))
		case "shutdown":
			return
		}
	}
}

type store struct {
	mu  sync.Mutex
	dir string
}

func (s *store) handleCall(msg rpc) rpc {
	fail := func(m string) rpc { return rpc{ID: msg.ID, Error: &rpcErr{Message: m}} }

	var p callParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return fail("bad params: " + err.Error())
	}
	if err := s.resolveDir(p); err != nil {
		return fail(err.Error())
	}
	key, _ := p.NamedArgs["key"].(string)
	prefix, _ := p.NamedArgs["prefix"].(string)

	s.mu.Lock()
	defer s.mu.Unlock()

	switch p.Operation {
	case "get":
		b, err := os.ReadFile(s.path(key))
		if os.IsNotExist(err) {
			return rpc{ID: msg.ID, Result: nil}
		}
		if err != nil {
			return fail(err.Error())
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return fail("corrupt value: " + err.Error())
		}
		return rpc{ID: msg.ID, Result: v}

	case "put":
		b, err := json.Marshal(p.NamedArgs["value"])
		if err != nil {
			return fail(err.Error())
		}
		fp := s.path(key)
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			return fail(err.Error())
		}
		tmp := fp + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			return fail(err.Error())
		}
		if err := os.Rename(tmp, fp); err != nil {
			return fail(err.Error())
		}
		return rpc{ID: msg.ID, Result: nil}

	case "delete":
		if err := os.Remove(s.path(key)); err != nil && !os.IsNotExist(err) {
			return fail(err.Error())
		}
		return rpc{ID: msg.ID, Result: nil}

	case "list":
		var keys []string
		_ = filepath.WalkDir(s.dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
				return nil
			}
			rel, err := filepath.Rel(s.dir, strings.TrimSuffix(path, ".json"))
			if err != nil {
				return nil
			}
			k := filepath.ToSlash(rel)
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
			return nil
		})
		return rpc{ID: msg.ID, Result: keys}

	default:
		return fail("unknown operation " + p.Operation)
	}
}

// resolveDir pins the storage root from the `dir` property of the
// `extension store` declaration on the first call.
func (s *store) resolveDir(p callParams) error {
	if s.dir != "" {
		return nil
	}
	for _, pr := range p.Declaration.Props {
		if pr.Name == "dir" {
			if d, ok := pr.Value.(string); ok && d != "" {
				s.dir = d
			}
		}
	}
	if s.dir == "" {
		d, err := os.MkdirTemp("", "mhl-store-fs-")
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "store-fs: no `dir` property — using ephemeral %s\n", d)
		s.dir = d
	}
	return os.MkdirAll(s.dir, 0o755)
}

func (s *store) path(key string) string {
	return filepath.Join(s.dir, filepath.FromSlash(key)) + ".json"
}
