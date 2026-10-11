package extension

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeExtension is a minimal in-process extension used to exercise the
// contract without any real protocol. It serves the "fake" kind and counts
// how many times Bind runs so the Instance cache can be observed.
type fakeExtension struct {
	binds int
}

func (f *fakeExtension) ID() string      { return "test.fake" }
func (f *fakeExtension) Version() string { return "0.0.1" }

func (f *fakeExtension) Declarations() []DeclarationSpec {
	return []DeclarationSpec{{
		Kind:          "fake",
		Documentation: "test-only extension",
		Properties: []PropertySpec{
			{Name: "greeting", Type: "string"},
		},
	}}
}

func (f *fakeExtension) Validate(decl Declaration) []Diagnostic {
	if _, ok := decl.StringProp("greeting"); !ok {
		return []Diagnostic{Errorf(decl.Pos, "fake-missing-greeting", "fake %q needs a string greeting", decl.Name)}
	}
	return nil
}

func (f *fakeExtension) Bind(decl Declaration, host HostContext) (Instance, error) {
	f.binds++
	greeting, _ := decl.StringProp("greeting")
	return &fakeInstance{greeting: greeting, host: host}, nil
}

type fakeInstance struct {
	greeting string
	host     HostContext
}

func (i *fakeInstance) Methods() []MethodSpec {
	return []MethodSpec{{
		Name:      "echo",
		Params:    []ParamSpec{{Name: "text", Type: "string"}},
		Returns:   "string",
		Signature: "echo(text: string) -> string",
	}}
}

func (i *fakeInstance) Call(ctx context.Context, req CallRequest) (Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch req.Method {
	case "echo":
		text, _ := req.Arg(0)
		return fmt.Sprintf("%s %v", i.greeting, text), nil
	default:
		return nil, fmt.Errorf("fake has no method %q", req.Method)
	}
}

func fakeDecl() Declaration {
	return Declaration{
		Kind:  "fake",
		Name:  "F",
		Props: []Property{{Name: "greeting", Value: "hi"}},
	}
}

func TestRegistryResolveAndCall(t *testing.T) {
	fake := &fakeExtension{}
	reg := NewRegistry(NopHost{})
	reg.Register(fake)

	got, err := reg.Call(context.Background(), CallRequest{
		Declaration: fakeDecl(),
		Method:      "echo",
		Args:        []Value{"world"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got != "hi world" {
		t.Fatalf("got %q, want %q", got, "hi world")
	}
}

func TestRegistryCachesInstancePerConfig(t *testing.T) {
	fake := &fakeExtension{}
	reg := NewRegistry(NopHost{})
	reg.Register(fake)

	if _, err := reg.Resolve(fakeDecl()); err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}
	if _, err := reg.Resolve(fakeDecl()); err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	if fake.binds != 1 {
		t.Fatalf("expected 1 Bind for identical config, got %d", fake.binds)
	}

	changed := fakeDecl()
	changed.Props[0].Value = "hey"
	if _, err := reg.Resolve(changed); err != nil {
		t.Fatalf("Resolve changed: %v", err)
	}
	if fake.binds != 2 {
		t.Fatalf("expected a fresh Bind for changed config, got %d binds", fake.binds)
	}
}

func TestRegistryUnknownKind(t *testing.T) {
	reg := NewRegistry(NopHost{})

	if _, err := reg.Resolve(Declaration{Kind: "nope", Name: "X"}); err == nil {
		t.Fatal("expected an error resolving an unregistered kind")
	}
	diags := reg.Validate(Declaration{Kind: "nope", Name: "X"})
	if len(diags) != 1 || diags[0].Code != "unknown-extension-kind" {
		t.Fatalf("expected one unknown-extension-kind diagnostic, got %#v", diags)
	}
}

func TestRegistryValidateDelegates(t *testing.T) {
	reg := NewRegistry(NopHost{})
	reg.Register(&fakeExtension{})

	if diags := reg.Validate(fakeDecl()); len(diags) != 0 {
		t.Fatalf("expected clean validation, got %#v", diags)
	}
	bad := Declaration{Kind: "fake", Name: "F"}
	if diags := reg.Validate(bad); len(diags) != 1 || diags[0].Code != "fake-missing-greeting" {
		t.Fatalf("expected fake-missing-greeting, got %#v", diags)
	}
}

func TestRegistryDuplicateKindPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic registering the same kind twice")
		}
	}()
	reg := NewRegistry(NopHost{})
	reg.Register(&fakeExtension{})
	reg.Register(&otherFakeSameKind{})
}

type otherFakeSameKind struct{}

func (o *otherFakeSameKind) ID() string      { return "test.other" }
func (o *otherFakeSameKind) Version() string { return "0.0.1" }
func (o *otherFakeSameKind) Declarations() []DeclarationSpec {
	return []DeclarationSpec{{Kind: "fake"}}
}
func (o *otherFakeSameKind) Validate(Declaration) []Diagnostic { return nil }
func (o *otherFakeSameKind) Bind(Declaration, HostContext) (Instance, error) {
	return nil, errors.New("unused")
}

func TestRegistryConcurrentResolve(t *testing.T) {
	fake := &fakeExtension{}
	reg := NewRegistry(NopHost{})
	reg.Register(fake)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := reg.Call(context.Background(), CallRequest{
				Declaration: fakeDecl(),
				Method:      "echo",
				Args:        []Value{"x"},
			}); err != nil {
				t.Errorf("concurrent Call: %v", err)
			}
		}()
	}
	wg.Wait()

	if fake.binds != 1 {
		t.Fatalf("expected exactly 1 Bind under concurrency, got %d", fake.binds)
	}
}

// kindExt is an external-style extension with a configurable id serving one
// kind — for exercising several providers of the same kind.
type kindExt struct{ id, kind string }

func (k *kindExt) ID() string      { return k.id }
func (k *kindExt) Version() string { return "0.0.1" }
func (k *kindExt) Declarations() []DeclarationSpec {
	return []DeclarationSpec{{Kind: k.kind}}
}
func (k *kindExt) Validate(Declaration) []Diagnostic { return nil }
func (k *kindExt) Bind(Declaration, HostContext) (Instance, error) {
	return &fakeInstance{greeting: k.id}, nil
}

func TestRegistryLookupByKindAndProvider(t *testing.T) {
	redis := &kindExt{id: "mhl.data.redis", kind: "data"}
	pg := &kindExt{id: "mhl.data.postgres", kind: "data"}
	sqlite := &kindExt{id: "mhl.state.sqlite", kind: "state"}

	tests := []struct {
		name      string
		exts      []Extension
		kind      string
		provider  string
		wantID    string
		wantCode  string
		wantInMsg string
	}{
		{name: "no provider registered", kind: "data", wantCode: "unknown-extension-kind"},
		{name: "single provider, no from", exts: []Extension{redis}, kind: "data", wantID: "mhl.data.redis"},
		{name: "single provider, matching from", exts: []Extension{redis}, kind: "data", provider: "mhl.data.redis", wantID: "mhl.data.redis"},
		{name: "two providers, no from", exts: []Extension{redis, pg}, kind: "data", wantCode: "ambiguous-extension-kind", wantInMsg: "mhl.data.postgres, mhl.data.redis"},
		{name: "two providers, from picks redis", exts: []Extension{redis, pg}, kind: "data", provider: "mhl.data.redis", wantID: "mhl.data.redis"},
		{name: "two providers, from picks postgres", exts: []Extension{redis, pg}, kind: "data", provider: "mhl.data.postgres", wantID: "mhl.data.postgres"},
		{name: "unknown provider", exts: []Extension{redis}, kind: "data", provider: "mhl.data.missing", wantCode: "unknown-extension-provider"},
		{name: "provider serves another kind", exts: []Extension{redis, sqlite}, kind: "data", provider: "mhl.state.sqlite", wantCode: "extension-provider-kind-mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewRegistry(NopHost{})
			for _, e := range tt.exts {
				if err := reg.TryRegister(e); err != nil {
					t.Fatalf("TryRegister %s: %v", e.ID(), err)
				}
			}
			ext, err := reg.Lookup(tt.kind, tt.provider)
			if tt.wantCode != "" {
				var le *LookupError
				if !errors.As(err, &le) || le.Code != tt.wantCode {
					t.Fatalf("err = %v, want code %q", err, tt.wantCode)
				}
				if tt.wantInMsg != "" && !strings.Contains(err.Error(), tt.wantInMsg) {
					t.Fatalf("err = %q, want it to mention %q", err, tt.wantInMsg)
				}
				diags := reg.Validate(Declaration{Kind: tt.kind, Provider: tt.provider, Name: "X"})
				if len(diags) != 1 || diags[0].Code != tt.wantCode {
					t.Fatalf("Validate = %+v, want one %q diagnostic", diags, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}
			if ext.ID() != tt.wantID {
				t.Fatalf("Lookup picked %q, want %q", ext.ID(), tt.wantID)
			}
		})
	}
}

func TestRegistryBuiltinKindIsReserved(t *testing.T) {
	reg := NewRegistry(NopHost{})
	reg.Register(&fakeExtension{}) // built-in style: claims "fake" exclusively
	if err := reg.TryRegister(&kindExt{id: "ext.fake", kind: "fake"}); err == nil {
		t.Fatal("expected an external extension to be refused a built-in's kind")
	}
}

func TestRegistrySameKindProvidersBindSeparately(t *testing.T) {
	reg := NewRegistry(NopHost{})
	for _, e := range []Extension{&kindExt{id: "a", kind: "data"}, &kindExt{id: "b", kind: "data"}} {
		if err := reg.TryRegister(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b"} {
		got, err := reg.Call(context.Background(), CallRequest{
			Declaration: Declaration{Kind: "data", Provider: id, Name: "D"},
			Method:      "echo",
			Args:        []Value{"x"},
		})
		if err != nil {
			t.Fatalf("Call via %s: %v", id, err)
		}
		if got != id+" x" {
			t.Fatalf("Call via %s = %q, want %q", id, got, id+" x")
		}
	}
}
