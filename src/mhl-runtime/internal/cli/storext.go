package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
	"github.com/mh-language/mhl-core-runtime/internal/extension/external"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/mcpserver"
)

// storeKind is the extension kind that backs `mhl serve mcp --http` durable
// state (sessions + run checkpoints) when a workflow directory declares one.
const storeKind = "store"

// discoverStoreExtension scans dir for a single `extension store <Name> { ... }`
// declaration, binds the installed extension that serves kind "store", and
// returns it wrapped as a mcpserver.KVStore. It returns (nil, noop, nil) when
// no such declaration exists — the on-disk `.mhl/state` default. logw receives
// the extension's own diagnostic lines.
func discoverStoreExtension(dir string, logw io.Writer) (mcpserver.KVStore, func(), error) {
	decl, ok, err := scanStoreDecl(dir)
	if err != nil {
		return nil, func() {}, err
	}
	if !ok {
		return nil, func() {}, nil
	}

	set, err := external.Discover(dir)
	if err != nil {
		return nil, func() {}, fmt.Errorf("resolving extensions for the %q store: %w", decl.Name, err)
	}
	for _, p := range set.Problems() {
		fmt.Fprintf(logw, "warning: extension %q not loaded: %s\n", p.ID, p.Message)
	}

	var chosen extension.Extension
	for _, ext := range set.Extensions() {
		for _, spec := range ext.Declarations() {
			if spec.Kind == storeKind {
				chosen = ext
			}
		}
	}
	if chosen == nil {
		set.CloseAll()
		return nil, func() {}, fmt.Errorf("workflow directory declares `extension store %s` but no installed extension serves kind %q (mhl extension install ...)", decl.Name, storeKind)
	}

	host := serveHost{client: http.DefaultClient, log: func(s string) { fmt.Fprintln(logw, s) }}
	inst, err := chosen.Bind(decl, host)
	if err != nil {
		set.CloseAll()
		return nil, func() {}, fmt.Errorf("binding store extension %q: %w", chosen.ID(), err)
	}

	// A store that advertises the "cas" capability in its handshake unlocks
	// cross-replica run locking (mcpserver.LockingKVStore). Absent it, the
	// server runs uncoordinated (single-writer) and says so.
	cas, claim, fence, scan := false, false, false, false
	if cr, ok := chosen.(interface {
		Capabilities(context.Context) ([]string, error)
	}); ok {
		if caps, cerr := cr.Capabilities(context.Background()); cerr == nil {
			cas = slices.Contains(caps, "cas")
			claim = slices.Contains(caps, "claim")
			fence = slices.Contains(caps, "fence")
			scan = slices.Contains(caps, "scan")
		} else {
			fmt.Fprintf(logw, "warning: store extension %q capability probe failed: %v\n", chosen.ID(), cerr)
		}
	}
	return &extKV{inst: inst, decl: decl, cas: cas, claim: claim, fence: fence, scan: scan}, set.CloseAll, nil
}

// scanStoreDecl walks dir's .mh files for exactly one `extension store` block
// and resolves its properties (string / number / bool literals and env(...) /
// vault(...) credential refs) to JSON values.
func scanStoreDecl(dir string) (extension.Declaration, bool, error) {
	var found []extension.Declaration
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".mh") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		prog, err := parser.Parse(string(src))
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		for _, decl := range prog.Decls {
			kind, name, props, ok := ast.AsExtension(decl)
			if !ok || kind != storeKind {
				continue
			}
			resolved, rerr := resolveStoreProps(props)
			if rerr != nil {
				return fmt.Errorf("%s: extension store %s: %w", path, name, rerr)
			}
			found = append(found, extension.Declaration{Kind: kind, Name: name, Props: resolved})
		}
		return nil
	})
	if walkErr != nil {
		return extension.Declaration{}, false, walkErr
	}
	switch len(found) {
	case 0:
		return extension.Declaration{}, false, nil
	case 1:
		return found[0], true, nil
	default:
		return extension.Declaration{}, false, fmt.Errorf("more than one `extension store` declaration in %s", dir)
	}
}

func resolveStoreProps(props []*ast.Property) ([]extension.Property, error) {
	out := make([]extension.Property, 0, len(props))
	for _, p := range props {
		if s, ok := ast.StringValue(p.Value); ok {
			out = append(out, extension.Property{Name: p.Name, Value: s})
			continue
		}
		if n, ok := ast.NumberValue(p.Value); ok {
			out = append(out, extension.Property{Name: p.Name, Value: n})
			continue
		}
		if b, ok := ast.BoolValue(p.Value); ok {
			out = append(out, extension.Property{Name: p.Name, Value: b})
			continue
		}
		if refs := ast.CredentialRefs(p.Value); len(refs) == 1 {
			v, err := auth.Resolve(refs[0])
			if err != nil {
				return nil, fmt.Errorf("resolving %s: %w", refs[0], err)
			}
			out = append(out, extension.Property{Name: p.Name, Value: v})
			continue
		}
		return nil, fmt.Errorf("property %q: only string/number/bool literals and a single env()/vault() ref are supported", p.Name)
	}
	return out, nil
}

// serveHost is the extension.HostContext for a host-side (non-interpreter)
// bind. It mirrors interpreter.inProcessHost — auth-backed secret resolution,
// a shared HTTP client, redacted logging.
type serveHost struct {
	client *http.Client
	log    func(string)
}

func (h serveHost) ResolveSecret(ref string) (string, error) { return auth.Resolve(ref) }
func (h serveHost) HTTPClient() *http.Client {
	if h.client != nil {
		return h.client
	}
	return http.DefaultClient
}
func (h serveHost) Logf(format string, args ...any) {
	if h.log != nil {
		h.log(auth.Redact(fmt.Sprintf(format, args...)))
	}
}
func (h serveHost) Redact(s string) string { return auth.Redact(s) }

// extKV adapts a bound `store` extension.Instance to mcpserver.KVStore (and,
// when the extension advertised the "cas" capability, mcpserver.LockingKVStore).
type extKV struct {
	inst  extension.Instance
	decl  extension.Declaration
	cas   bool
	claim bool // extension advertised "claim" (native claim_next)
	fence bool // extension advertised "fence" (atomic put_fenced / delete_fenced)
	scan  bool // extension advertised "scan" (list_statuses / count_pending)
}

func (k *extKV) call(ctx context.Context, method string, named map[string]extension.Value) (extension.Value, error) {
	return k.inst.Call(ctx, extension.CallRequest{Declaration: k.decl, Method: method, NamedArgs: named})
}

func (k *extKV) Get(ctx context.Context, key string) ([]byte, bool, error) {
	v, err := k.call(ctx, "get", map[string]extension.Value{"key": key})
	if err != nil {
		return nil, false, err
	}
	if v == nil {
		return nil, false, nil
	}
	b, err := json.Marshal(v)
	return b, err == nil, err
}

func (k *extKV) Put(ctx context.Context, key string, value any) error {
	_, err := k.call(ctx, "put", map[string]extension.Value{"key": key, "value": value})
	return err
}

func (k *extKV) Delete(ctx context.Context, key string) error {
	_, err := k.call(ctx, "delete", map[string]extension.Value{"key": key})
	return err
}

func (k *extKV) List(ctx context.Context, prefix string) ([]string, error) {
	v, err := k.call(ctx, "list", map[string]extension.Value{"prefix": prefix})
	if err != nil {
		return nil, err
	}
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (k *extKV) CASCapable() bool { return k.cas }

func (k *extKV) PutIfAbsent(ctx context.Context, key string, value any) (bool, error) {
	if !k.cas {
		return false, mcpserver.ErrCASUnsupported
	}
	v, err := k.call(ctx, "put_if_absent", map[string]extension.Value{"key": key, "value": value})
	if err != nil {
		return false, err
	}
	acquired, _ := v.(bool)
	return acquired, nil
}

func (k *extKV) CompareAndSwap(ctx context.Context, key string, expected []byte, newValue any) (bool, error) {
	if !k.cas {
		return false, mcpserver.ErrCASUnsupported
	}
	v, err := k.call(ctx, "compare_and_swap", map[string]extension.Value{
		"key": key, "expected": string(expected), "value": newValue,
	})
	if err != nil {
		return false, err
	}
	swapped, _ := v.(bool)
	return swapped, nil
}

// ClaimNext implements mcpserver.ClaimNexter over the extension's optional
// `claim_next(prefix, holder)` method (e.g. Postgres `SELECT … FOR UPDATE SKIP
// LOCKED`). Returns ErrClaimNextUnsupported when the extension did not advertise
// "claim", so the caller falls back to the generic CAS scan.
func (k *extKV) ClaimNext(ctx context.Context, holder string) (string, mcpserver.RunStatusRec, bool, error) {
	if !k.claim {
		return "", mcpserver.RunStatusRec{}, false, mcpserver.ErrClaimNextUnsupported
	}
	v, err := k.call(ctx, "claim_next", map[string]extension.Value{"prefix": "run/", "holder": holder})
	if err != nil {
		return "", mcpserver.RunStatusRec{}, false, err
	}
	obj, ok := v.(map[string]any)
	if !ok || obj == nil {
		return "", mcpserver.RunStatusRec{}, false, nil // no pending run
	}
	keyStr, _ := obj["key"].(string)
	id := strings.TrimSuffix(strings.TrimPrefix(keyStr, "run/"), "/status")
	var rec mcpserver.RunStatusRec
	if raw, mErr := json.Marshal(obj["value"]); mErr == nil {
		_ = json.Unmarshal(raw, &rec)
	}
	if id == "" {
		return "", mcpserver.RunStatusRec{}, false, fmt.Errorf("claim_next returned an unparseable key %q", keyStr)
	}
	return id, rec, true, nil
}

// FenceCapable reports whether the extension advertised "fence".
func (k *extKV) FenceCapable() bool { return k.fence }

// PutFenced implements mcpserver.FencedWriter over the extension's optional
// `put_fenced(key, value, lock_key, holder, token)` — one atomic statement that
// writes only while the lease record at lock_key still names this holder+token.
func (k *extKV) PutFenced(ctx context.Context, key string, value any, lockKey, holder, token string) (bool, error) {
	v, err := k.call(ctx, "put_fenced", map[string]extension.Value{
		"key": key, "value": value, "lock_key": lockKey, "holder": holder, "token": token,
	})
	if err != nil {
		return false, err
	}
	written, _ := v.(bool)
	return written, nil
}

// DeleteFenced is PutFenced's counterpart for removing a key.
func (k *extKV) DeleteFenced(ctx context.Context, key, lockKey, holder, token string) (bool, error) {
	v, err := k.call(ctx, "delete_fenced", map[string]extension.Value{
		"key": key, "lock_key": lockKey, "holder": holder, "token": token,
	})
	if err != nil {
		return false, err
	}
	deleted, _ := v.(bool)
	return deleted, nil
}

// ListStatusRecs implements the mcpserver status-scan fast path: every
// run/<id>/status record in one round-trip (Postgres: a single SELECT), so the
// durable-intake reconcile / sweep loops do not do a list plus a get per run.
// supported is false when the extension did not advertise "scan".
func (k *extKV) ListStatusRecs(ctx context.Context) (map[string]mcpserver.RunStatusRec, bool, error) {
	if !k.scan {
		return nil, false, nil
	}
	v, err := k.call(ctx, "list_statuses", map[string]extension.Value{"prefix": "run/"})
	if err != nil {
		return nil, true, err
	}
	arr, _ := v.([]any)
	out := make(map[string]mcpserver.RunStatusRec, len(arr))
	for _, x := range arr {
		obj, ok := x.(map[string]any)
		if !ok {
			continue
		}
		keyStr, _ := obj["key"].(string)
		id := strings.TrimSuffix(strings.TrimPrefix(keyStr, "run/"), "/status")
		if id == "" || strings.Contains(id, "/") {
			continue
		}
		var rec mcpserver.RunStatusRec
		if raw, mErr := json.Marshal(obj["value"]); mErr == nil {
			_ = json.Unmarshal(raw, &rec)
		}
		out[id] = rec
	}
	return out, true, nil
}

// CountPendingRuns implements the pending-depth fast path: a COUNT the store
// serves from its partial index on pending status rows, so the /metrics gauge
// never scans the whole run set. supported is false without the "scan"
// capability.
func (k *extKV) CountPendingRuns(ctx context.Context) (int, bool, error) {
	if !k.scan {
		return 0, false, nil
	}
	v, err := k.call(ctx, "count_pending", map[string]extension.Value{"prefix": "run/"})
	if err != nil {
		return 0, true, err
	}
	return extValueInt(v), true, nil
}

// extValueInt coerces a JSON-decoded extension result to an int (numbers arrive
// as float64 over the wire; json.Number when a decoder keeps them).
func extValueInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}
