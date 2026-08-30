package interpreter

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// echoExtension is an in-process test extension serving the "echo" kind. It
// exists only so the interpreter's generic dispatch path can be exercised
// without importing internal/features/mcp or internal/features/a2a.
type echoExtension struct{}

func (echoExtension) ID() string      { return "test.echo" }
func (echoExtension) Version() string { return "0.0.1" }

func (echoExtension) Declarations() []extension.DeclarationSpec {
	return []extension.DeclarationSpec{{
		Kind:       "echo",
		Properties: []extension.PropertySpec{{Name: "prefix", Type: "string"}},
	}}
}

func (echoExtension) Validate(extension.Declaration) []extension.Diagnostic { return nil }

func (echoExtension) Bind(decl extension.Declaration, _ extension.HostContext) (extension.Instance, error) {
	prefix, _ := decl.StringProp("prefix")
	return echoInstance{prefix: prefix}, nil
}

type echoInstance struct{ prefix string }

func (echoInstance) Methods() []extension.MethodSpec {
	return []extension.MethodSpec{{Name: "say", Signature: "say(text: string) -> string"}}
}

func (i echoInstance) Call(_ context.Context, req extension.CallRequest) (extension.Value, error) {
	switch req.Method {
	case "say":
		text, _ := req.Arg(0)
		return fmt.Sprintf("%s%v", i.prefix, text), nil
	case "config_prefix":
		return i.prefix, nil
	default:
		return nil, fmt.Errorf("echo has no method %q", req.Method)
	}
}

// evalWithRegistry parses a whole program, then evaluates a single expression
// against an evalCtx wired to reg.
func evalWithRegistry(t *testing.T, programSrc, exprSrc string, reg *extension.Registry) (any, error) {
	t.Helper()
	prog, err := parser.Parse(programSrc)
	if err != nil {
		t.Fatalf("parse program: %v", err)
	}
	expr, err := parser.ParseExpr(exprSrc)
	if err != nil {
		t.Fatalf("parse expr %q: %v", exprSrc, err)
	}
	ctx := &evalCtx{prog: prog, out: io.Discard, env: Env{}, registry: reg}
	return evalExpr(ctx, expr)
}

func TestGenericExtensionDispatch(t *testing.T) {
	reg := extension.NewRegistry(extension.NopHost{})
	reg.Register(echoExtension{})

	const program = `
extension echo Greeter {
    prefix: "hi "
}
`
	got, err := evalWithRegistry(t, program, `Greeter.say("world")`, reg)
	if err != nil {
		t.Fatalf("Greeter.say: %v", err)
	}
	if got != "hi world" {
		t.Fatalf("got %q, want %q", got, "hi world")
	}
}

// TestExtensionPropertiesEvaluated proves a declaration's property values go
// through the full expression evaluator (here: string concatenation), not a
// bare-literal reader.
func TestExtensionPropertiesEvaluated(t *testing.T) {
	reg := extension.NewRegistry(extension.NopHost{})
	reg.Register(echoExtension{})

	const program = `
extension echo Greeter {
    prefix: "a" + "b" + "-"
}
`
	got, err := evalWithRegistry(t, program, `Greeter.config_prefix()`, reg)
	if err != nil {
		t.Fatalf("config_prefix: %v", err)
	}
	if got != "ab-" {
		t.Fatalf("resolved prefix = %q, want %q", got, "ab-")
	}
}

func TestGenericExtensionUnknownKind(t *testing.T) {
	reg := extension.NewRegistry(extension.NopHost{}) // nothing registered

	const program = `
extension crm Customer {
    endpoint: "https://example.test"
}
`
	_, err := evalWithRegistry(t, program, `Customer.lookup("1")`, reg)
	if err == nil {
		t.Fatal("expected an error calling into an unregistered extension kind")
	}
}

// TestExtensionDeclResolution: findExtensionDecl resolves any `extension`
// declaration by name, regardless of kind.
func TestExtensionDeclResolution(t *testing.T) {
	prog, err := parser.Parse(`
extension mcp Repo { url: "x" }
extension a2a Remote { url: "y" }
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := findExtensionDecl(prog, "Repo"); !ok {
		t.Fatal("findExtensionDecl should resolve an extension mcp node")
	}
	if _, ok := findExtensionDecl(prog, "Remote"); !ok {
		t.Fatal("findExtensionDecl should resolve an extension a2a node")
	}
}
