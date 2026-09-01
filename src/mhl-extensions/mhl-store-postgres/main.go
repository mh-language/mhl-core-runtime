// mhl-store-postgres is an official `store`-kind mhl extension backed by
// PostgreSQL: one row per key in a `(key text primary key, value jsonb,
// updated_at timestamptz)` table.
//
// It is a drop-in replacement for tests/extensions/store-fs and
// src/mhl-store-s3: the same wire contract (kind `store`; get / put / delete /
// list over newline-delimited JSON-RPC on stdin/stdout — see
// tests/extensions/extension-protocol.md). `put` is an atomic
// `INSERT ... ON CONFLICT DO UPDATE`, so a `mhl serve mcp --http` checkpoint
// write can never leave a half-written row.
//
//	extension store S {
//	    dsn:                env("DATABASE_URL")     // or the discrete fields below
//	    host:              "localhost"
//	    port:              "5432"
//	    dbname:            "mhl_state"
//	    user:              "mhl"
//	    password:          env("PGPASSWORD")
//	    sslmode:           "disable"                // prefer | require | verify-full ...
//	    table:             "mhl_store"              // default; may be "schema.table"
//	    prefix:            ""                       // optional key namespace in the table
//	    max_conns:          8
//	    statement_timeout: "10s"                    // server-side, per statement
//	    auto_migrate:       true                    // CREATE TABLE/INDEX IF NOT EXISTS on first use
//	    log:              "/tmp/store-postgres.jsonl"
//	}
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	extID      = "dev.mhl.store-postgres"
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
			}})
		case "call":
			// Pin the pool from the first call's props synchronously, in the
			// read loop, before choosing concurrent dispatch — a lazy config
			// inside the goroutine races the loop reading the next line.
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
	pg     *pgStore
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
		raw, ok, err := s.pg.get(ctx, key)
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
		if err := s.pg.put(ctx, key, b); err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: nil}
		}

	case "delete":
		if err := s.pg.del(ctx, key); err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: nil}
		}

	case "list":
		keys, err := s.pg.list(ctx, prefix)
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

// config pins the pool from the declaration's props on the first call.
// Idempotent; a bad config is remembered in cfgErr and fails every call with a
// single clear message.
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
		var stmtTimeout time.Duration
		if v := gets("statement_timeout"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				stmtTimeout = d
			} else {
				s.cfgErr = fmt.Errorf("store-postgres: bad `statement_timeout` %q: %v", v, err)
				s.logEvent(map[string]any{"ev": "init", "pid": os.Getpid(), "err": errStr(s.cfgErr)})
				return
			}
		}

		cfg := pgConfig{
			DSN:              gets("dsn"),
			Host:             gets("host"),
			Port:             portString(props["port"]),
			DBName:           gets("dbname"),
			User:             gets("user"),
			Password:         gets("password"),
			SSLMode:          gets("sslmode"),
			Table:            gets("table"),
			Prefix:           gets("prefix"),
			MaxConns:         int32(numProp(props["max_conns"])),
			StatementTimeout: stmtTimeout,
			AutoMigrate:      autoMigrate,
		}
		s.pg, s.cfgErr = newPGStore(context.Background(), cfg)

		s.logEvent(map[string]any{
			"ev": "init", "pid": os.Getpid(),
			"table":  firstNonEmpty(cfg.Table, defaultTable),
			"target": dsnTarget(cfg),
			"err":    errStr(s.cfgErr),
		})
	})
}

func (s *store) shutdown(via string) {
	if s.pg != nil {
		s.pg.close()
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

// portString accepts a port given as either a JSON number or a string.
func portString(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case float64:
		return strconv.Itoa(int(n))
	case json.Number:
		return n.String()
	}
	return ""
}

// dsnTarget is a non-secret "host/db" summary for the init log line — never
// the password.
func dsnTarget(c pgConfig) string {
	if c.DSN != "" {
		if i := strings.Index(c.DSN, "@"); i >= 0 {
			return c.DSN[i+1:]
		}
		return "(dsn)"
	}
	host := firstNonEmpty(c.Host, "localhost")
	if c.Port != "" {
		host += ":" + c.Port
	}
	return host + "/" + c.DBName
}
