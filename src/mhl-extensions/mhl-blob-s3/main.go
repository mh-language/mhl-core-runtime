// mhl-blob-s3 is an official `blob`-kind mhl extension: general-purpose object
// storage backed by Amazon S3 (or any S3-compatible endpoint — MinIO,
// Cloudflare R2, Ceph RGW). Not a `store` KV backend for `mhl serve` — a plain
// capability a `.mh` workflow reaches for.
//
//	extension blob Files {
//	    provider:          "s3"                        // optional; only "s3" today
//	    bucket:            "my-bucket"
//	    endpoint:          env("S3_ENDPOINT")          // MinIO/R2; omit for real AWS
//	    region:            "us-east-1"
//	    prefix:            "reports/"                   // optional key namespace
//	    access_key_id:     env("AWS_ACCESS_KEY_ID")
//	    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
//	}
//
//	pipeline P {
//	    step upload {
//	        Files.put("q3/summary.csv", csv, "text/csv")
//	        var meta = Files.head("q3/summary.csv")     // { size, etag, content_type, last_modified }
//	        var url  = Files.presign_get("q3/summary.csv", "15m")
//	    }
//	}
//
// Methods: put(key, body[, content_type]) · put_bytes(key, base64[, content_type])
// · get(key) -> string|null · get_bytes(key) -> base64|null
// · head(key) -> {size,etag,content_type,last_modified}|null · exists(key) -> bool
// · delete(key) · list(prefix[, delimiter]) -> {keys:[...], common_prefixes:[...]}
// · copy(src, dst) · presign_get(key, expires) -> url
// · presign_put(key, expires[, content_type]) -> url
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	extID      = "dev.mhl.blob-s3"
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
	s := &blob{}

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
	wg.Wait()
	s.logEvent(map[string]any{"ev": "shutdown", "calls": s.calls.Load(), "via": "eof"})
}

type blob struct {
	once   sync.Once
	logMu  sync.Mutex
	cli    *s3Client
	cfgErr error
	prefix string
	logF   *os.File
	calls  atomic.Int64
}

func (s *blob) handleCall(msg rpc) rpc {
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
	case "put":
		res = s.put(ctx, msg.ID, s.objKey(key), []byte(p.strArg("body", 1)), p.strArg("content_type", 2), fail)

	case "put_bytes":
		raw, derr := base64.StdEncoding.DecodeString(p.strArg("base64", 1))
		if derr != nil {
			res = fail("put_bytes: invalid base64: " + derr.Error())
			break
		}
		res = s.put(ctx, msg.ID, s.objKey(key), raw, p.strArg("content_type", 2), fail)

	case "get":
		b, _, ok, err := s.cli.getObject(ctx, s.objKey(key))
		switch {
		case err != nil:
			res = fail(err.Error())
		case !ok:
			res = rpc{ID: msg.ID, Result: nil}
		default:
			res = rpc{ID: msg.ID, Result: string(b)}
		}

	case "get_bytes":
		b, _, ok, err := s.cli.getObject(ctx, s.objKey(key))
		switch {
		case err != nil:
			res = fail(err.Error())
		case !ok:
			res = rpc{ID: msg.ID, Result: nil}
		default:
			res = rpc{ID: msg.ID, Result: base64.StdEncoding.EncodeToString(b)}
		}

	case "head":
		m, ok, err := s.cli.headObject(ctx, s.objKey(key))
		switch {
		case err != nil:
			res = fail(err.Error())
		case !ok:
			res = rpc{ID: msg.ID, Result: nil}
		default:
			res = rpc{ID: msg.ID, Result: metaMap(key, m)}
		}

	case "exists":
		_, ok, err := s.cli.headObject(ctx, s.objKey(key))
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: ok}
		}

	case "delete":
		if err := s.cli.deleteObject(ctx, s.objKey(key)); err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: nil}
		}

	case "list":
		res = s.list(ctx, msg.ID, p.strArg("prefix", 0), p.strArg("delimiter", 1), fail)

	case "copy":
		src := s.objKey(p.strArg("src", 0))
		dst := s.objKey(p.strArg("dst", 1))
		if err := s.cli.copyObject(ctx, src, dst); err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: nil}
		}

	case "presign_get", "presign_put":
		method := "GET"
		var extraSigned map[string]string
		if p.Operation == "presign_put" {
			method = "PUT"
			if ct := p.strArg("content_type", 2); ct != "" {
				extraSigned = map[string]string{"content-type": ct}
			}
		}
		d, derr := parseExpires(p.arg("expires", 1))
		if derr != nil {
			res = fail(derr.Error())
			break
		}
		u, err := s.cli.presign(ctx, method, s.objKey(key), d, extraSigned)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: u}
		}

	default:
		res = fail("unknown operation " + p.Operation)
	}

	s.logEvent(map[string]any{
		"ev": "call", "seq": n, "decl": p.Declaration.Name, "op": p.Operation,
		"key": key, "dur_us": time.Since(start).Microseconds(), "err": res.Error != nil,
	})
	return res
}

func (s *blob) put(ctx context.Context, id json.RawMessage, objKey string, body []byte, ct string, fail func(string) rpc) rpc {
	if err := s.cli.putObject(ctx, objKey, body, ct); err != nil {
		return fail(err.Error())
	}
	return rpc{ID: id, Result: nil}
}

func (s *blob) list(ctx context.Context, id json.RawMessage, logicalPrefix, delimiter string, fail func(string) rpc) rpc {
	objs, cps, err := s.cli.listObjects(ctx, s.prefix+logicalPrefix, delimiter)
	if err != nil {
		return fail(err.Error())
	}
	keys := make([]any, 0, len(objs))
	for _, o := range objs {
		lk := strings.TrimPrefix(o.Key, s.prefix)
		keys = append(keys, map[string]any{
			"key": lk, "size": o.Size, "etag": o.ETag, "last_modified": o.LastModified,
		})
	}
	prefixes := make([]any, 0, len(cps))
	for _, cp := range cps {
		prefixes = append(prefixes, strings.TrimPrefix(cp, s.prefix))
	}
	return rpc{ID: id, Result: map[string]any{"keys": keys, "common_prefixes": prefixes}}
}

func metaMap(logicalKey string, m objMeta) map[string]any {
	return map[string]any{
		"key": logicalKey, "size": m.Size, "etag": m.ETag,
		"content_type": m.ContentType, "last_modified": m.LastModified,
	}
}

// objKey maps a logical blob key to its S3 object key (prefix + key, verbatim
// — no synthetic suffix).
func (s *blob) objKey(key string) string { return s.prefix + key }

// parseExpires accepts a Go duration string ("15m", "1h") or a number of
// seconds. Capped at 7 days (the SigV4 presign maximum).
func parseExpires(v any) (time.Duration, error) {
	var d time.Duration
	switch t := v.(type) {
	case string:
		if pd, err := time.ParseDuration(t); err == nil {
			d = pd
		} else {
			return 0, fmt.Errorf("expires: %q is not a duration (\"15m\") or a number of seconds", t)
		}
	case float64:
		d = time.Duration(t) * time.Second
	case json.Number:
		f, _ := t.Float64()
		d = time.Duration(f) * time.Second
	case nil:
		return 0, fmt.Errorf("expires is required (\"15m\" or a number of seconds)")
	default:
		return 0, fmt.Errorf("expires: unsupported type %T", v)
	}
	if d <= 0 {
		return 0, fmt.Errorf("expires must be positive")
	}
	if max := 7 * 24 * time.Hour; d > max {
		d = max
	}
	return d, nil
}

func (s *blob) config(p callParams) {
	s.once.Do(func() {
		props := map[string]any{}
		for _, pr := range p.Declaration.Props {
			props[pr.Name] = pr.Value
		}
		gets := func(k string) string { v, _ := props[k].(string); return v }

		if prov := gets("provider"); prov != "" && prov != "s3" {
			s.cfgErr = fmt.Errorf("blob-s3: provider %q is not supported (only \"s3\")", prov)
			return
		}

		prefix := gets("prefix")
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
		maxRetries := -1
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
			"creds": credMode(props), "err": errStr(s.cfgErr),
		})
	})
}

func (s *blob) logEvent(m map[string]any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.logF == nil {
		return
	}
	m["t"] = time.Now().UnixNano()
	b, _ := json.Marshal(m)
	_, _ = s.logF.Write(append(b, '\n'))
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
