package parser

import "testing"

func TestInternalModifierParses(t *testing.T) {
	prog, err := Parse(`
tool T {
    root(): string -> self.clean(" a ")
    internal clean(s: string): string -> s.trim()
    internal(x: string): string -> x
}
export internal workflow Flow { step A { log("x") } }
internal partial workflow Split { entry step A { log("y") } }
internal workflow Hidden = Flow
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m := prog.Decls[0].Tool.Methods
	if m[0].Internal || !m[1].Internal || m[1].Name != "clean" {
		t.Fatalf("methods = %+v %+v", m[0], m[1])
	}
	// A method may still be *named* internal.
	if m[2].Internal || m[2].Name != "internal" {
		t.Fatalf("a method named `internal` = %+v", m[2])
	}
	if d := prog.Decls[1]; !d.Export || !d.Pipeline.Internal {
		t.Fatalf("export internal workflow = %+v", d)
	}
	if p := prog.Decls[2].Pipeline; !p.Internal || !p.Partial {
		t.Fatalf("internal partial workflow = %+v", p)
	}
	if a := prog.Decls[3].Alias; a == nil || !a.Internal {
		t.Fatalf("internal alias = %+v", a)
	}
}
