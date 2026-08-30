package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Registry holds the extensions available to one mhl execution and caches the
// Instance bound for each declaration. It is built once per execution (see
// the interpreter's registryFor) and is safe for concurrent use — a
// `parallel` pipeline block runs steps, and therefore extension calls, from
// several goroutines.
type Registry struct {
	host HostContext

	mu      sync.Mutex
	byKind  map[string]Extension
	byID    map[string]Extension
	ordered []Extension

	instMu    sync.Mutex
	instances map[string]Instance
}

// NewRegistry returns an empty Registry that binds instances against host.
func NewRegistry(host HostContext) *Registry {
	return &Registry{
		host:      host,
		byKind:    map[string]Extension{},
		byID:      map[string]Extension{},
		instances: map[string]Instance{},
	}
}

// Register adds ext, claiming every declaration kind it serves. Registering a
// kind twice, or an ID twice, is a programming error and panics: the set of
// built-in extensions is fixed at build time.
func (r *Registry) Register(ext Extension) {
	if err := r.TryRegister(ext); err != nil {
		panic("extension: " + err.Error())
	}
}

// TryRegister is Register without the panic — for extensions that are not
// fixed at build time (external ones discovered from a lock file), where an
// id or kind collision must fail that one extension, not the whole process.
func (r *Registry) TryRegister(ext Extension) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.byID[ext.ID()]; dup {
		return fmt.Errorf("ID %q registered twice", ext.ID())
	}
	for _, spec := range ext.Declarations() {
		if prev, dup := r.byKind[spec.Kind]; dup {
			return fmt.Errorf("kind %q claimed by both %q and %q", spec.Kind, prev.ID(), ext.ID())
		}
	}
	r.byID[ext.ID()] = ext
	for _, spec := range ext.Declarations() {
		r.byKind[spec.Kind] = ext
	}
	r.ordered = append(r.ordered, ext)
	return nil
}

// Lookup returns the extension serving the given declaration kind.
func (r *Registry) Lookup(kind string) (Extension, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ext, ok := r.byKind[kind]
	return ext, ok
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

// Validate runs the owning extension's static checks for one declaration. An
// unknown kind is itself a single error diagnostic.
func (r *Registry) Validate(decl Declaration) []Diagnostic {
	ext, ok := r.Lookup(decl.Kind)
	if !ok {
		return []Diagnostic{Errorf(decl.Pos, "unknown-extension-kind",
			"no extension registered for kind %q", decl.Kind)}
	}
	return ext.Validate(decl)
}

// Resolve returns the Instance for decl, binding it on first use and reusing
// it thereafter for the life of the Registry. Binding is keyed by the
// declaration's kind, name, and the content of its resolved properties, so a
// re-resolve with identical config hits the cache while a changed config does
// not.
func (r *Registry) Resolve(decl Declaration) (Instance, error) {
	ext, ok := r.Lookup(decl.Kind)
	if !ok {
		return nil, fmt.Errorf("no extension registered for kind %q", decl.Kind)
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
	fmt.Fprintf(h, "%s\x00%s\x00", decl.Kind, decl.Name)
	// json.Marshal of the property slice is deterministic for the
	// JSON-compatible values that cross this boundary (object keys are
	// sorted), which is all Property.Value is allowed to hold.
	if b, err := json.Marshal(decl.Props); err == nil {
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}
