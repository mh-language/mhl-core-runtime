package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry holds the extensions available to one mhl execution and caches the
// Instance bound for each declaration. It is built once per execution (see
// the interpreter's registryFor) and is safe for concurrent use — a
// `parallel` pipeline block runs steps, and therefore extension calls, from
// several goroutines.
type Registry struct {
	host HostContext

	mu       sync.Mutex
	byKind   map[string][]Extension
	byID     map[string]Extension
	reserved map[string]bool // kinds claimed via Register (built-ins): never shared
	ordered  []Extension

	instMu    sync.Mutex
	instances map[string]Instance
}

// NewRegistry returns an empty Registry that binds instances against host.
func NewRegistry(host HostContext) *Registry {
	return &Registry{
		host:      host,
		byKind:    map[string][]Extension{},
		byID:      map[string]Extension{},
		reserved:  map[string]bool{},
		instances: map[string]Instance{},
	}
}

// Register adds a built-in ext, claiming every declaration kind it serves
// exclusively: no other extension, built-in or external, may serve the same
// kind afterwards. Registering a kind twice, or an ID twice, is a programming
// error and panics: the set of built-in extensions is fixed at build time.
func (r *Registry) Register(ext Extension) {
	if err := r.register(ext, true); err != nil {
		panic("extension: " + err.Error())
	}
}

// TryRegister is Register without the panic — for extensions that are not
// fixed at build time (external ones discovered from a lock file), where an
// id collision must fail that one extension, not the whole process. Unlike
// Register, the kinds it claims are shared: several external extensions may
// serve the same kind (mhl.data.redis and mhl.data.postgres both serve
// `data`), and a declaration picks one with `from "<id>"` (see Lookup). A
// kind reserved by a built-in is still refused.
func (r *Registry) TryRegister(ext Extension) error {
	return r.register(ext, false)
}

func (r *Registry) register(ext Extension, exclusive bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.byID[ext.ID()]; dup {
		return fmt.Errorf("ID %q registered twice", ext.ID())
	}
	for _, spec := range ext.Declarations() {
		prev := r.byKind[spec.Kind]
		if len(prev) == 0 {
			continue
		}
		if exclusive || r.reserved[spec.Kind] {
			return fmt.Errorf("kind %q claimed by both %q and %q", spec.Kind, prev[0].ID(), ext.ID())
		}
	}
	r.byID[ext.ID()] = ext
	for _, spec := range ext.Declarations() {
		r.byKind[spec.Kind] = append(r.byKind[spec.Kind], ext)
		if exclusive {
			r.reserved[spec.Kind] = true
		}
	}
	r.ordered = append(r.ordered, ext)
	return nil
}

// LookupError explains why Lookup could not pick an extension. Code is one of
// "unknown-extension-kind", "unknown-extension-provider",
// "extension-provider-kind-mismatch" or "ambiguous-extension-kind" — the same
// codes Validate reports as diagnostics.
type LookupError struct {
	Code    string
	Message string
}

func (e *LookupError) Error() string { return e.Message }

// Lookup returns the extension serving a declaration of the given kind. With
// a provider (the id named by `extension <kind> <Name> from "<id>"`), it is
// that extension, which must serve kind. Without one, kind must be served by
// exactly one registered extension — several is an ambiguity the declaration
// resolves by naming a provider.
func (r *Registry) Lookup(kind, provider string) (Extension, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if provider != "" {
		ext, ok := r.byID[provider]
		if !ok {
			return nil, &LookupError{Code: "unknown-extension-provider",
				Message: fmt.Sprintf("no extension registered with id %q", provider)}
		}
		for _, spec := range ext.Declarations() {
			if spec.Kind == kind {
				return ext, nil
			}
		}
		return nil, &LookupError{Code: "extension-provider-kind-mismatch",
			Message: fmt.Sprintf("extension %q does not serve kind %q", provider, kind)}
	}

	switch exts := r.byKind[kind]; len(exts) {
	case 0:
		return nil, &LookupError{Code: "unknown-extension-kind",
			Message: fmt.Sprintf("no extension registered for kind %q", kind)}
	case 1:
		return exts[0], nil
	default:
		ids := make([]string, len(exts))
		for i, e := range exts {
			ids[i] = e.ID()
		}
		sort.Strings(ids)
		return nil, &LookupError{Code: "ambiguous-extension-kind",
			Message: fmt.Sprintf("kind %q is served by %s; name one with `from \"<id>\"`", kind, strings.Join(ids, ", "))}
	}
}

// Extensions returns the registered extensions in registration order.
func (r *Registry) Extensions() []Extension {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Extension, len(r.ordered))
	copy(out, r.ordered)
	return out
}

// Specs returns every declaration spec across all registered extensions,
// sorted by kind. Consumed by lint and the LSP so neither has to know which
// extension owns which kind.
func (r *Registry) Specs() []DeclarationSpec {
	var out []DeclarationSpec
	for _, ext := range r.Extensions() {
		out = append(out, ext.Declarations()...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// Validate runs the owning extension's static checks for one declaration. A
// declaration Lookup cannot resolve (unknown kind or provider, ambiguous kind)
// is itself a single error diagnostic.
func (r *Registry) Validate(decl Declaration) []Diagnostic {
	ext, err := r.Lookup(decl.Kind, decl.Provider)
	if err != nil {
		code := "unknown-extension-kind"
		var le *LookupError
		if errors.As(err, &le) {
			code = le.Code
		}
		return []Diagnostic{Errorf(decl.Pos, code, "%s", err.Error())}
	}
	return ext.Validate(decl)
}

// Resolve returns the Instance for decl, binding it on first use and reusing
// it thereafter for the life of the Registry. Binding is keyed by the
// declaration's kind, provider, name, and the content of its resolved properties, so a
// re-resolve with identical config hits the cache while a changed config does
// not.
func (r *Registry) Resolve(decl Declaration) (Instance, error) {
	ext, err := r.Lookup(decl.Kind, decl.Provider)
	if err != nil {
		return nil, err
	}

	key := declKey(decl)

	r.instMu.Lock()
	defer r.instMu.Unlock()
	if inst, cached := r.instances[key]; cached {
		return inst, nil
	}
	inst, err := ext.Bind(decl, r.host)
	if err != nil {
		return nil, err
	}
	r.instances[key] = inst
	return inst, nil
}

// Call resolves decl and invokes one method on it.
func (r *Registry) Call(ctx context.Context, req CallRequest) (Value, error) {
	inst, err := r.Resolve(req.Declaration)
	if err != nil {
		return nil, err
	}
	return inst.Call(ctx, req)
}

// declKey is a stable identity for a declaration + its resolved config.
func declKey(decl Declaration) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00", decl.Kind, decl.Provider, decl.Name)
	// json.Marshal of the property slice is deterministic for the
	// JSON-compatible values that cross this boundary (object keys are
	// sorted), which is all Property.Value is allowed to hold.
	if b, err := json.Marshal(decl.Props); err == nil {
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}
