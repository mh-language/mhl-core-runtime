// mhl-store-redis is an official `store`-kind mhl extension backed by Redis:
// durable key/value state for `mhl serve mcp --http` (sessions, `run/*`
// checkpoints, `run/*` locks). Drop-in for src/mhl-store-postgres and
// src/mhl-store-s3 — the same wire contract (kind `store`; get / put / delete
// / list over newline-delimited JSON-RPC on stdin/stdout).
//
// It implements the optional "cas" capability (advertised in the initialize
// handshake): put_if_absent (SET ... NX) and compare_and_swap (a Lua EVAL),
// single atomic operations the `mhl serve` cross-replica run lock is built on.
//
// Sibling: src/mhl-extensions/mhl-cache-redis is the `cache`-kind Redis
// extension (TTL, incr, expire) — a different kind, installable alongside.
//
//	extension store S {
//	    url: env("REDIS_URL")   // redis://[user:pass@]host:port/db  (rediss:// = TLS)
//	    # or: addr / username / password / db / tls
//	}
//
// Methods: get(key) -> any|null · put(key, value) · delete(key) ·
// list(prefix) -> [string] · put_if_absent(key, value) -> bool ·
// compare_and_swap(key, expected, value) -> bool
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	extID      = "dev.mhl.store-redis"
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
		Kind  string `json:"kind"`
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
	s := &server{}

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
				// layer enables cross-replica run locking only for a store that
				// advertises it.
				"capabilities": []string{"cas"},
			}})
		case "call":
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
	wg.Wait()
	s.shutdown("eof")
}

type server struct {
	once   sync.Once
	logMu  sync.Mutex
	store  *redisStore
	cfgErr error
	logF   *os.File
	calls  atomic.Int64
}

func (s *server) handleCall(msg rpc) rpc {
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

	var res rpc
	switch p.Operation {
	case "get":
		raw, ok, err := s.store.get(ctx, key)
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
		res = okOrFail(msg.ID, s.store.put(ctx, key, b), fail)

	case "delete":
		res = okOrFail(msg.ID, s.store.del(ctx, key), fail)

	case "list":
		keys, err := s.store.list(ctx, p.strArg("prefix", 0))
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: keys}
		}

	case "put_if_absent":
		b, err := json.Marshal(p.arg("value", 1))
		if err != nil {
			res = fail(err.Error())
			break
		}
		acquired, err := s.store.putIfAbsent(ctx, key, b)
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
		swapped, err := s.store.compareAndSwap(ctx, key, exp, b)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: swapped}
		}

	default:
		res = fail("redis: unknown operation " + p.Operation)
	}

	s.logEvent(map[string]any{
		"ev": "call", "seq": n, "decl": p.Declaration.Name, "op": p.Operation,
		"key": key, "dur_us": time.Since(start).Microseconds(), "err": res.Error != nil,
	})
	return res
}

func okOrFail(id json.RawMessage, err error, fail func(string) rpc) rpc {
	if err != nil {
		return fail(err.Error())
	}
	return rpc{ID: id, Result: nil}
}

// jsonBytes returns the raw JSON text for a call argument. The mhl serve layer
// sends compare_and_swap's `expected` as a string holding the exact bytes a
// prior `get` returned; a `.mh` caller might pass a value directly, so
// anything else is marshalled.
func jsonBytes(v any) []byte {
	if s, ok := v.(string); ok {
		return []byte(s)
	}
	b, _ := json.Marshal(v)
	return b
}

func (s *server) config(p callParams) {
	s.once.Do(func() {
		props := map[string]any{}
		for _, pr := range p.Declaration.Props {
			props[pr.Name] = pr.Value
		}
		gets := func(k string) string { v, _ := props[k].(string); return v }
		boolp := func(k string) bool { v, _ := props[k].(bool); return v }

		if lp := gets("log"); lp != "" {
			if f, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				s.logF = f
			}
		}

		cc := clientConfig{
			Addr:          gets("addr"),
			Username:      gets("username"),
			Password:      gets("password"),
			DB:            int(numArg(props["db"])),
			TLS:           boolp("tls"),
			TLSSkipVerify: boolp("tls_skip_verify"),
			PoolSize:      int(numArg(props["pool_size"])),
		}
		if u := gets("url"); u != "" {
			if err := applyURL(&cc, u); err != nil {
				s.cfgErr = err
				s.logEvent(map[string]any{"ev": "init", "pid": os.Getpid(), "err": errStr(err)})
				return
			}
		}
		for name, dst := range map[string]*time.Duration{"dial_timeout": &cc.DialTimeout, "read_timeout": &cc.ReadTimeout} {
			if v := gets(name); v != "" {
				d, err := time.ParseDuration(v)
				if err != nil {
					s.cfgErr = fmt.Errorf("redis: bad `%s` %q: %v", name, v, err)
					s.logEvent(map[string]any{"ev": "init", "pid": os.Getpid(), "err": errStr(s.cfgErr)})
					return
				}
				*dst = d
			}
		}

		s.store, s.cfgErr = newRedisStore(context.Background(), storeConfig{
			clientConfig: cc,
			KeyPrefix:    gets("key_prefix"),
		})
		s.logEvent(map[string]any{
			"ev": "init", "pid": os.Getpid(), "addr": redacted(cc.Addr),
			"db": cc.DB, "tls": cc.TLS, "prefix": gets("key_prefix"),
			"err": errStr(s.cfgErr),
		})
	})
}

func (s *server) shutdown(via string) {
	if s.store != nil {
		s.store.close()
	}
	s.logEvent(map[string]any{"ev": "shutdown", "calls": s.calls.Load(), "via": via})
}

func (s *server) logEvent(m map[string]any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.logF == nil {
		return
	}
	m["t"] = time.Now().UnixNano()
	b, _ := json.Marshal(m)
	_, _ = s.logF.Write(append(b, '\n'))
}

// applyURL fills cc from redis://[user:pass@]host:port/db (rediss:// => TLS).
func applyURL(cc *clientConfig, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("redis: bad `url` %q: %w", raw, err)
	}
	switch u.Scheme {
	case "redis":
	case "rediss":
		cc.TLS = true
	default:
		return fmt.Errorf("redis: `url` scheme must be redis:// or rediss://, got %q", u.Scheme)
	}
	if u.Host != "" {
		cc.Addr = u.Host
	}
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			cc.Username = name
		}
		if pw, ok := u.User.Password(); ok {
			cc.Password = pw
		}
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return fmt.Errorf("redis: `url` db segment %q is not a number", p)
		}
		cc.DB = n
	}
	return nil
}

func redacted(addr string) string {
	if i := strings.Index(addr, "@"); i >= 0 {
		return addr[i+1:]
	}
	return addr
}

func errStr(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

func numArg(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}
