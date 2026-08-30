package extension

import (
	"context"
	"errors"
	"fmt"
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
