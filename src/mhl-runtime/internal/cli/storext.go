package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	return &extKV{inst: inst, decl: decl}, set.CloseAll, nil
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

// extKV adapts a bound `store` extension.Instance to mcpserver.KVStore.
type extKV struct {
	inst extension.Instance
	decl extension.Declaration
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
