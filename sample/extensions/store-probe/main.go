// store-probe is an instrumented `store`-kind extension for exercising the mhl
// external-extension mechanism — the language surface (`extension store S { ... }`
// called from `.mh` steps, including `parallel`) and the `mhl serve mcp --http`
// durable-state path — under load.
//
// It is a superset of sample/extensions/store-fs: same wire contract (kind
// `store`; get/put/delete/list; one JSON file per key under `dir`, atomic
// rename), plus knobs read from the declaration's own properties so a test can
// tune behaviour without touching the manifest:
//
//	extension store S {
//	    dir:         "/path"      // storage root (persists across processes)
//	    log:         "/path.jsonl"// append one JSON line per handled message
//	    latency_ms:  200          // sleep this long inside every call
//	    crash_after: 5            // os.Exit(1) after N calls (chaos / restart test)
//	    serial:      true         // handle one call at a time (store-fs behaviour);
//	                              // default is a goroutine per call (concurrent)
//	}
//
// Protocol: newline-delimited JSON-RPC on stdin/stdout — see
// sample/extensions/extension-protocol.md.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	extID      = "dev.mhl.store-probe"
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
		Name  string `json:"name"`
		Props []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"props"`
	} `json:"declaration"`
	Operation string         `json:"operation"`
	Args      []any          `json:"args"`
	NamedArgs map[string]any `json:"named_args"`
}

// arg resolves a call argument by named key or, failing that, by position —
// the `.mh` language path (`S.put("k", v)`) sends positional args, the
// `mhl serve mcp` KV adapter sends named ones.
func (p callParams) arg(name string, pos int) any {
	if v, ok := p.NamedArgs[name]; ok {
		return v
	}
	if pos >= 0 && pos < len(p.Args) {
		return p.Args[pos]
	}
	return nil
}

func (p callParams) strArg(name string, pos int) string {
	s, _ := p.arg(name, pos).(string)
	return s
}

func main() {
	s := &store{}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 8<<20)

	var outMu sync.Mutex
	enc := json.NewEncoder(os.Stdout)
	send := func(m rpc) { outMu.Lock(); _ = enc.Encode(m); outMu.Unlock() }

	var wg sync.WaitGroup
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
			send(rpc{ID: msg.ID, Result: map[string]any{
				"api_version": apiVersion,
				"extension":   map[string]string{"id": extID, "version": extVersion},
			}})
		case "call":
			// Pin config from the first call's props *synchronously*, in the
			// read loop, before choosing serial vs. concurrent dispatch — a
			// lazy config inside the goroutine races the loop reading the
			// next lines.
			var p callParams
			_ = json.Unmarshal(msg.Params, &p)
			s.config(p)

			handle := func() {
				defer wg.Done()
				send(s.handleCall(msg))
			}
			wg.Add(1)
			if s.serialize {
				handle()
			} else {
				go handle()
			}
		case "shutdown":
			wg.Wait()
			s.logEvent(map[string]any{"ev": "shutdown", "calls": s.calls.Load(), "via": "notify"})
			return
		}
	}
	// stdin closed without an explicit shutdown notification (the host closes
	// the pipe and may SIGKILL almost immediately — graceful shutdown is
	// best-effort). Flush a marker anyway.
	wg.Wait()
	s.logEvent(map[string]any{"ev": "shutdown", "calls": s.calls.Load(), "via": "eof"})
}

type store struct {
	once      sync.Once
	mu        sync.Mutex // guards the on-disk map
	logMu     sync.Mutex
	cfgMu     sync.Mutex
	dir       string
	logF      *os.File
	lat       time.Duration
	crash     int64 // crash when calls > crash; -1 disables
	serialize bool
	calls     atomic.Int64
}

func (s *store) handleCall(msg rpc) rpc {
	fail := func(m string) rpc { return rpc{ID: msg.ID, Error: &rpcErr{Message: m}} }

	var p callParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return fail("bad params: " + err.Error())
	}
	s.config(p)

	n := s.calls.Add(1)
	start := time.Now()

	if s.crash >= 0 && n > s.crash {
		s.logEvent(map[string]any{"ev": "crash", "after": n - 1})
		fmt.Fprintf(os.Stderr, "store-probe: crash_after=%d reached — exiting 1\n", s.crash)
		os.Exit(1)
	}
	if s.lat > 0 {
		time.Sleep(s.lat)
	}

	key := p.strArg("key", 0)
	prefix := p.strArg("prefix", 0)

	var res rpc
	switch p.Operation {
	case "get":
		res = s.get(msg.ID, key, fail)
	case "put":
		res = s.put(msg.ID, key, p.arg("value", 1), fail)
	case "delete":
		res = s.del(msg.ID, key, fail)
	case "list":
		res = s.list(msg.ID, prefix)
	default:
		res = fail("unknown operation " + p.Operation)
	}

	s.logEvent(map[string]any{
		"ev": "call", "seq": n, "decl": p.Declaration.Name,
		"op": p.Operation, "key": firstNonEmpty(key, prefix),
		"dur_us": time.Since(start).Microseconds(),
	})
	return res
}

func (s *store) get(id json.RawMessage, key string, fail func(string) rpc) rpc {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path(key))
	if os.IsNotExist(err) {
		return rpc{ID: id, Result: nil}
	}
	if err != nil {
		return fail(err.Error())
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return fail("corrupt value: " + err.Error())
	}
	return rpc{ID: id, Result: v}
}

func (s *store) put(id json.RawMessage, key string, value any, fail func(string) rpc) rpc {
	b, err := json.Marshal(value)
	if err != nil {
		return fail(err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fp := s.path(key)
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return fail(err.Error())
	}
	tmp := fmt.Sprintf("%s.%d.tmp", fp, s.calls.Load())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fail(err.Error())
	}
	if err := os.Rename(tmp, fp); err != nil {
		return fail(err.Error())
	}
	return rpc{ID: id, Result: nil}
}

func (s *store) del(id json.RawMessage, key string, fail func(string) rpc) rpc {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(key)); err != nil && !os.IsNotExist(err) {
		return fail(err.Error())
	}
	return rpc{ID: id, Result: nil}
}

func (s *store) list(id json.RawMessage, prefix string) rpc {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := []string{}
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
	return rpc{ID: id, Result: keys}
}

// config pins dir / log / latency / crash / serial from the declaration's
// props on the first call. Idempotent.
func (s *store) config(p callParams) {
	s.once.Do(func() {
		s.cfgMu.Lock()
		defer s.cfgMu.Unlock()
		props := map[string]any{}
		for _, pr := range p.Declaration.Props {
			props[pr.Name] = pr.Value
		}
		if d, ok := props["dir"].(string); ok && d != "" {
			s.dir = d
		}
		if s.dir == "" {
			d, err := os.MkdirTemp("", "mhl-store-probe-")
			if err == nil {
				s.dir = d
			}
			fmt.Fprintf(os.Stderr, "store-probe: no `dir` property — using ephemeral %s\n", s.dir)
		}
		_ = os.MkdirAll(s.dir, 0o755)

		if lp, ok := props["log"].(string); ok && lp != "" {
			if f, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				s.logF = f
			}
		}
		s.lat = time.Duration(numProp(props["latency_ms"])) * time.Millisecond
		if _, set := props["crash_after"]; set {
			s.crash = int64(numProp(props["crash_after"]))
		} else {
			s.crash = -1
		}
		if b, ok := props["serial"].(bool); ok {
			s.serialize = b
		}
		s.logEvent(map[string]any{"ev": "init", "pid": os.Getpid(), "dir": s.dir,
			"latency_ms": int64(s.lat / time.Millisecond), "crash_after": s.crash, "serial": s.serialize})
	})
}

func (s *store) logEvent(m map[string]any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.logF == nil {
		return
	}
	m["t"] = time.Now().UnixNano()
	b, _ := json.Marshal(m)
	_, _ = s.logF.Write(append(b, '\n'))
}

func (s *store) path(key string) string {
	return filepath.Join(s.dir, filepath.FromSlash(key)) + ".json"
}

func numProp(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		var f float64
		_, _ = fmt.Sscanf(n, "%g", &f)
		return f
	}
	return 0
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
