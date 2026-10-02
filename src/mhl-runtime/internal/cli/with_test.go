package cli_test

import (
	"strings"
	"testing"
)

// obj with { field: value, ... } copies obj and overrides the listed
// fields, leaving the original untouched. These tests run real programs
// through `mhl run`.

func TestWithCopiesAndOverridesFields(t *testing.T) {
	out, err := run(t, wrapStep(`
        var d = { titulo: "x", avisos: [], outro: 1 }
        var d2 = d with { avisos: ["a"] }
        log("d.avisos=" + json.stringify(d.avisos.size()))
        log("d2.avisos=" + json.stringify(d2.avisos.size()))
        log("d2.titulo=" + d2.titulo)
        log("d2.outro=" + json.stringify(d2.outro))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"d.avisos=0", "d2.avisos=1", "d2.titulo=x", "d2.outro=1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q (with must copy-and-override, leaving the original untouched):\n%s", want, out)
		}
	}
}

func TestWithChainsLeftToRight(t *testing.T) {
	out, err := run(t, wrapStep(`
        var chained = { a: 1 } with { b: 2 } with { c: 3 }
        log("chained=" + json.stringify(chained))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `chained={"a":1,"b":2,"c":3}`) {
		t.Fatalf("chained with must apply each continuation on the previous result:\n%s", out)
	}
}

func TestWithPreservesSharedRefIdentity(t *testing.T) {
	out, err := run(t, wrapStep(`
        var r = ref { count: 0 }
        var obj = { r: r, x: 1 }
        var obj2 = obj with { x: 2 }
        r["count"] = r.count + 1
        log("obj2.r.count=" + json.stringify(obj2.r.count))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// obj2.r must be the SAME ref as r (not a copy) — its own contract
	// ("copied, not shared") only applies to the with-copy's plain fields.
	if !strings.Contains(out, "obj2.r.count=1") {
		t.Fatalf("with must preserve a ref-typed field's shared identity:\n%s", out)
	}
}

func TestWithOnANonObjectErrors(t *testing.T) {
	_, err := run(t, wrapStep(`var bad = 5 with { x: 1 }`))
	if err == nil || !strings.Contains(err.Error(), "with: left side must be an object, got number") {
		t.Fatalf("expected a clear non-object error, got: %v", err)
	}
}

// `with` is contextual, like `ref`: only `with {` starts a with-expression,
// so it stays usable as an ordinary identifier everywhere else.
func TestWithStaysUsableAsAnIdentifier(t *testing.T) {
	out, err := run(t, wrapStep(`
        var with = 5
        log("ident=" + json.stringify(with + 1))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "ident=6") {
		t.Fatalf("`with` must stay usable as a plain identifier:\n%s", out)
	}
}
