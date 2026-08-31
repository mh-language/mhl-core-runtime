// mhl-store-s3 is an official `store`-kind mhl extension backed by Amazon S3
// (or any S3-compatible object store — MinIO, Cloudflare R2, Ceph RGW).
//
// It is a drop-in replacement for sample/extensions/store-fs: the same wire
// contract (kind `store`; get / put / delete / list over newline-delimited
// JSON-RPC on stdin/stdout — see sample/extensions/extension-protocol.md),
// storing each key as one S3 object at `<prefix><key>.json`. So a
// `mhl serve mcp --http` run checkpoint lands at, e.g.,
// `s3://<bucket>/mhl/run/<id>/checkpoint/<pipeline>.json`.
//
//	extension store S {
//	    bucket:            "mhl-state"
//	    endpoint:          "http://localhost:9000"    // omit for real AWS S3
//	    region:            "us-east-1"
//	    prefix:            "mhl/"                      // key namespace in the bucket
//	    access_key_id:     env("AWS_ACCESS_KEY_ID")
//	    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
//	    session_token:     env("AWS_SESSION_TOKEN")   // optional (STS creds)
//	    force_path_style:  true                        // implied when endpoint is set
//	    log:              "/tmp/store-s3.jsonl"        // optional wire trace
//	}
//
// The four operations and the JSON-RPC framing are the whole contract; the
// storage is the only thing that changes versus store-fs.
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
	extID      = "dev.mhl.store-s3"
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
			// Pin the S3 client from the first call's props synchronously,
			// in the read loop, before choosing concurrent dispatch — a lazy
			// config inside the goroutine races the loop reading the next line.
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
			s.logEvent(map[string]any{"ev": "shutdown", "calls": s.calls.Load(), "via": "notify"})
			return
		}
	}
	// stdin closed without an explicit shutdown (the host closes the pipe and
	// may SIGKILL almost immediately — graceful shutdown is best-effort).
	wg.Wait()
	s.logEvent(map[string]any{"ev": "shutdown", "calls": s.calls.Load(), "via": "eof"})
}

type store struct {
	once   sync.Once
	logMu  sync.Mutex
	cli    *s3Client
	cfgErr error
	prefix string
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
		b, ok, err := s.cli.getObject(ctx, s.objKey(key))
		switch {
		case err != nil:
			res = fail(err.Error())
		case !ok:
			res = rpc{ID: msg.ID, Result: nil}
		default:
			var v any
			if err := json.Unmarshal(b, &v); err != nil {
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
		if err := s.cli.putObject(ctx, s.objKey(key), b); err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: nil}
		}

	case "delete":
		if err := s.cli.deleteObject(ctx, s.objKey(key)); err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: nil}
		}

	case "list":
		keys, err := s.listLogical(ctx, prefix)
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

// objKey maps a logical store key to its S3 object key.
func (s *store) objKey(key string) string { return s.prefix + key + ".json" }

// listLogical returns every logical key under logicalPrefix, i.e. every S3
// object under `<prefix><logicalPrefix>` with the `.json` suffix stripped.
func (s *store) listLogical(ctx context.Context, logicalPrefix string) ([]string, error) {
	objs, err := s.cli.listKeys(ctx, s.prefix+logicalPrefix)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, o := range objs {
		if !strings.HasPrefix(o, s.prefix) || !strings.HasSuffix(o, ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(o, s.prefix), ".json"))
	}
	return out, nil
}

// config pins the S3 client from the declaration's props on the first call.
// Idempotent; a bad config is remembered in cfgErr and fails every call with
// a single clear message.
func (s *store) config(p callParams) {
	s.once.Do(func() {
		props := map[string]any{}
		for _, pr := range p.Declaration.Props {
			props[pr.Name] = pr.Value
		}
		gets := func(k string) string { v, _ := props[k].(string); return v }

		prefix := "mhl/"
		if v, ok := props["prefix"].(string); ok {
			prefix = v
		}
		if prefix != "" && !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		s.prefix = prefix

		if lp := gets("log"); lp != "" {
			if f, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				s.logF = f
			}
		}

		boolProp := func(k string) bool { b, _ := props[k].(bool); return b }
		maxRetries := -1 // -1 => client default (3)
		if _, set := props["max_retries"]; set {
			maxRetries = int(numProp(props["max_retries"]))
		}

		s.cli, s.cfgErr = newS3Client(s3Config{
			Bucket:               gets("bucket"),
			Endpoint:             gets("endpoint"),
			Region:               gets("region"),
			ForcePathStyle:       boolProp("force_path_style"),
			AccessKeyID:          gets("access_key_id"),
			SecretKey:            gets("secret_access_key"),
			SessionToken:         gets("session_token"),
			WebIdentityTokenFile: gets("web_identity_token_file"),
			RoleARN:              gets("role_arn"),
			RoleSessionName:      gets("role_session_name"),
			UseIMDS:              boolProp("use_imds"),
			MaxRetries:           maxRetries,
		})

		s.logEvent(map[string]any{
			"ev": "init", "pid": os.Getpid(), "bucket": gets("bucket"),
			"endpoint": gets("endpoint"), "prefix": s.prefix,
			"creds": credMode(props),
			"err":   errStr(s.cfgErr),
		})
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

// credMode names the credential source that newS3Client will pick, for the
// init log line — never the secret values themselves.
func credMode(props map[string]any) string {
	s := func(k string) string { v, _ := props[k].(string); return v }
	b := func(k string) bool { v, _ := props[k].(bool); return v }
	switch {
	case s("access_key_id") != "" && s("secret_access_key") != "":
		return "static"
	case s("web_identity_token_file") != "" && s("role_arn") != "":
		return "web_identity"
	case b("use_imds"):
		return "imds"
	default:
		return "anonymous"
	}
}
