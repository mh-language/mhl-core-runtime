// mhl-store-sqlite is an official `store`-kind mhl extension backed by
// SQLite: one row per key in a `(key TEXT PRIMARY KEY, value TEXT,
// updated_at TIMESTAMP)` table inside a local database file.
//
// It is a drop-in replacement for tests/extensions/store-fs and
// src/mhl-store-postgres: the same wire contract (kind `store`; get / put /
// delete / list over newline-delimited JSON-RPC on stdin/stdout — see
// tests/extensions/extension-protocol.md). `put` is an atomic
// `INSERT ... ON CONFLICT DO UPDATE`, so a `mhl serve mcp --http` checkpoint
// write can never leave a half-written row. It also implements the optional
// "cas" capability (advertised in the initialize handshake): put_if_absent
// and compare_and_swap, single atomic statements the `mhl serve`
// cross-replica run lock is built on.
//
// The driver is modernc.org/sqlite — pure Go, no CGO — so the release
// targets cross-compile with CGO_ENABLED=0. Being file-local, this store is
// single-host: state is not shared between machines (use mhl-store-postgres
// or mhl-store-redis for that).
//
//	extension store S {
//	    path:           env("MHL_STORE_PATH")   // or a literal "state/mhl.db"
//	    table:          "mhl_store"             // default
//	    prefix:         ""                      // optional key namespace in the table
//	    busy_timeout_ms: 5000                   // wait on a locked database
//	    journal_mode:   "WAL"                   // DELETE | TRUNCATE | PERSIST | MEMORY | OFF
//	    auto_migrate:    true                   // CREATE TABLE IF NOT EXISTS on first use
//	    log:            "/tmp/store-sqlite.jsonl"
//	}
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	extID      = "dev.mhl.store-sqlite"
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

// arg resolves a call argument by named key or, failing that, by position.
// The `.mh` language path (`S.put("k", v)`) sends positional args; the
// `mhl serve mcp` KV adapter sends named ones ("key", "value", "prefix").
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
				// "cas": atomic put_if_absent / compare_and_swap. The mhl serve
				// layer enables cross-replica run locking only when a store
				// advertises this.
				"capabilities": []string{"cas"},
			}})
		case "call":
			// Pin the database from the first call's props synchronously, in
			// the read loop, before choosing concurrent dispatch — a lazy
			// config inside the goroutine races the loop reading the next
			// line.
			var p callParams
			_ = json.Unmarshal(msg.Params, &p)
			s.config(p)

			wg.Add(1)
			go func() {
				defer wg.Done()
				send(s.handleCall(msg))
			}()
		case "shutdown":
			wg.Wait()
			s.shutdown("notify")
			return
		}
	}
	// stdin closed without an explicit shutdown (the host closes the pipe and
	// may SIGKILL almost immediately — graceful shutdown is best-effort).
	wg.Wait()
	s.shutdown("eof")
}

type store struct {
	once   sync.Once
	logMu  sync.Mutex
	sq     *sqliteStore
	cfgErr error
	logF   *os.File
	calls  atomic.Int64
}

func (s *store) handleCall(msg rpc) rpc {
	fail := func(m string) rpc { return rpc{ID: msg.ID, Error: &rpcErr{Message: m}} }

	var p callParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return fail("bad params: " + err.Error())
	}
	if s.cfgErr != nil {
		return fail(s.cfgErr.Error())
	}

	ctx := context.Background()
	n := s.calls.Add(1)
	start := time.Now()
	key := p.strArg("key", 0)
	prefix := p.strArg("prefix", 0)

	var res rpc
	switch p.Operation {
	case "get":
		raw, ok, err := s.sq.get(ctx, key)
		switch {
		case err != nil:
			res = fail(err.Error())
		case !ok:
			res = rpc{ID: msg.ID, Result: nil}
		default:
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				res = fail("corrupt value at " + key + ": " + err.Error())
			} else {
				res = rpc{ID: msg.ID, Result: v}
			}
		}

	case "put":
		b, err := json.Marshal(p.arg("value", 1))
		if err != nil {
			res = fail(err.Error())
			break
		}
		if err := s.sq.put(ctx, key, b); err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: nil}
		}

	case "put_if_absent":
		b, err := json.Marshal(p.arg("value", 1))
		if err != nil {
			res = fail(err.Error())
			break
		}
		acquired, err := s.sq.putIfAbsent(ctx, key, b)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: acquired}
		}

	case "compare_and_swap":
		exp := jsonBytes(p.arg("expected", 1))
		b, err := json.Marshal(p.arg("value", 2))
		if err != nil {
			res = fail(err.Error())
			break
		}
		swapped, err := s.sq.compareAndSwap(ctx, key, exp, b)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: swapped}
		}

	case "delete":
		if err := s.sq.del(ctx, key); err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: nil}
		}

	case "list":
		keys, err := s.sq.list(ctx, prefix)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: keys}
		}

	default:
		res = fail("unknown operation " + p.Operation)
	}

	s.logEvent(map[string]any{
		"ev": "call", "seq": n, "decl": p.Declaration.Name, "op": p.Operation,
		"key": firstNonEmpty(key, prefix), "dur_us": time.Since(start).Microseconds(),
		"err": res.Error != nil,
	})
	return res
}

// config pins the database from the declaration's props on the first call.
// Idempotent; a bad config is remembered in cfgErr and fails every call with
// a single clear message.
func (s *store) config(p callParams) {
	s.once.Do(func() {
		props := map[string]any{}
		for _, pr := range p.Declaration.Props {
			props[pr.Name] = pr.Value
		}
		gets := func(k string) string { v, _ := props[k].(string); return v }

		if lp := gets("log"); lp != "" {
			if f, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				s.logF = f
			}
		}

		autoMigrate := true
		if b, ok := props["auto_migrate"].(bool); ok {
			autoMigrate = b
		}

		cfg := sqliteConfig{
			Path:          gets("path"),
			Table:         gets("table"),
			Prefix:        gets("prefix"),
			BusyTimeoutMS: int(numProp(props["busy_timeout_ms"])),
			JournalMode:   gets("journal_mode"),
			AutoMigrate:   autoMigrate,
		}
		s.sq, s.cfgErr = newSQLiteStore(context.Background(), cfg)

		s.logEvent(map[string]any{
			"ev": "init", "pid": os.Getpid(),
			"table": firstNonEmpty(cfg.Table, defaultTable),
			"path":  cfg.Path,
			"err":   errStr(s.cfgErr),
		})
	})
}

func (s *store) shutdown(via string) {
	if s.sq != nil {
		s.sq.close()
	}
	s.logEvent(map[string]any{"ev": "shutdown", "calls": s.calls.Load(), "via": via})
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

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// jsonBytes returns the raw JSON text for a call argument. The mhl serve
// layer sends compare_and_swap's `expected` as a string holding the exact
// bytes a prior `get` returned; a `.mh` caller might pass a value directly,
// so anything else is marshalled.
func jsonBytes(v any) []byte {
	if s, ok := v.(string); ok {
		return []byte(s)
	}
	b, _ := json.Marshal(v)
	return b
}

func errStr(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

// numProp coerces a declaration property value (JSON number, or a numeric
// string) to a float64.
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
