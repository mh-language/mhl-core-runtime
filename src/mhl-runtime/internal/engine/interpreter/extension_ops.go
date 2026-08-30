package interpreter

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// findExtensionDecl locates an `extension <kind> <Name> { ... }` declaration
// by name, via ast.AsExtension.
func findExtensionDecl(prog *ast.Program, name string) (*ast.Declaration, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if _, declName, _, ok := ast.AsExtension(decl); ok && declName == name {
			return decl, true
		}
	}
	return nil, false
}

// evalExtensionCall dispatches `<Name>.<member>(...)` on a declared extension
// through the per-execution ExtensionRegistry: it resolves the declaration's
// properties to values (fail-closed on any unresolved credential), binds (or
// reuses) the extension Instance, evaluates the call arguments, and forwards
// them as an extension.CallRequest.
//
// The adapter owns its own error wording — it formats `<Name>.<method>: ...`
// exactly as the dedicated dispatch used to — so
// this function returns the adapter's error verbatim rather than wrapping it.
func evalExtensionCall(ctx *evalCtx, decl *ast.Declaration, member string, call *ast.Call, depth int) (any, error) {
	kind, name, _, ok := ast.AsExtension(decl)
	if !ok {
		return nil, fmt.Errorf("%s is not an extension declaration", member)
	}

	edecl, err := resolveExtensionDeclaration(ctx, decl, member, depth)
	if err != nil {
		return nil, err
	}

	reg := extRegistryOf(ctx)
	if _, ok := reg.Lookup(kind); !ok {
		return nil, fmt.Errorf("%s.%s: no extension registered for kind %q", name, member, kind)
	}

	args, err := evalCallArgs(ctx, call, depth)
	if err != nil {
		return nil, err
	}

	return reg.Call(goctxOf(ctx), extension.CallRequest{
		Declaration: edecl,
		Method:      member,
		Args:        args.positional,
		NamedArgs:   args.named,
	})
}

// resolveExtensionDeclaration evaluates an extension declaration's property
// bag into the DTO the contract exposes. Property values go through the full
// expression evaluator (so `env(...)`, concatenation and object literals work
// as they do in any declaration body); a bare duration literal — an
// `extension a2a`'s `poll_interval:` — is read directly, since the evaluator
// rejects durations in expression position.
//
// Before evaluating, every credential reference reachable in a property
// (`env("KEY")`, including `"Bearer " + env("KEY")`) is resolved through
// internal/features/auth and fails closed on a missing value — reproducing
// the old BuildRegistryWithError guarantee, now uniformly for every
// extension kind and also registering the resolved secret for redaction.
func resolveExtensionDeclaration(ctx *evalCtx, decl *ast.Declaration, member string, depth int) (extension.Declaration, error) {
	kind, name, props, _ := ast.AsExtension(decl)

	out := make([]extension.Property, 0, len(props))
	for _, p := range props {
		for _, ref := range ast.CredentialRefs(p.Value) {
			if _, err := auth.Resolve(ref); err != nil {
				return extension.Declaration{}, fmt.Errorf("%s.%s: %w", name, member, err)
			}
		}

		var v any
		if d, ok := ast.DurationValue(p.Value); ok {
			v = d
		} else {
			ev, err := evalExprAt(ctx, p.Value, depth)
			if err != nil {
				return extension.Declaration{}, fmt.Errorf("%s.%s: property %q: %w", name, member, p.Name, err)
			}
			v = ev
		}
		out = append(out, extension.Property{
			Name:  p.Name,
			Value: v,
			Pos:   posToExt(p.Pos),
		})
	}

	pos := extension.Position{}
	if decl.Extension != nil {
		pos = posToExt(decl.Extension.Pos)
	}
	return extension.Declaration{Kind: kind, Name: name, Props: out, Pos: pos}, nil
}

func posToExt(p lexer.Position) extension.Position {
	return extension.Position{File: p.Filename, Line: p.Line, Column: p.Column}
}
