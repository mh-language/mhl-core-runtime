package types_test

import (
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

func aliasesOf(t *testing.T, src string) (map[string]types.Type, []types.AliasError) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return types.Aliases(prog)
}

func TestAliasesResolveSimpleTargets(t *testing.T) {
	m, errs := aliasesOf(t, `
type Slug = string
type Ids  = string[]
type Grid = number[][]
type Pt   = { x: number, y: number }
`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if got := m["Slug"]; !got.Equal(types.String) {
		t.Errorf("Slug = %s, want string", got)
	}
	if got := m["Ids"]; !got.Equal(types.ArrayOf(types.String)) {
		t.Errorf("Ids = %s, want string[]", got)
	}
	if got := m["Grid"]; !got.Equal(types.ArrayOf(types.ArrayOf(types.Number))) {
		t.Errorf("Grid = %s, want number[][]", got)
	}
	if got := m["Pt"]; !got.Equal(types.ObjectOf(map[string]types.Type{"x": types.Number, "y": types.Number})) {
		t.Errorf("Pt = %s", got)
	}
}

func TestAliasesResolveThroughOtherAliases(t *testing.T) {
	m, errs := aliasesOf(t, `
type A = string
type B = A
type C = B[]
`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if got := m["C"]; !got.Equal(types.ArrayOf(types.String)) {
		t.Errorf("C = %s, want string[]", got)
	}
}

func TestAliasesReportCycle(t *testing.T) {
	_, errs := aliasesOf(t, `
type A = B
type B = A
`)
	if len(errs) != 2 {
		t.Fatalf("want 2 errors, got %+v", errs)
	}
	for _, e := range errs {
		if !strings.Contains(e.Message, "cyclic") {
			t.Errorf("want cyclic message, got %q", e.Message)
		}
	}
}

func TestAliasesReportUnknownTarget(t *testing.T) {
	_, errs := aliasesOf(t, `type X = sting`)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, `unrecognized type "sting"`) {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestAliasesReportDuplicate(t *testing.T) {
	_, errs := aliasesOf(t, `
type X = string
type X = number
`)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "already declared") {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestAliasesReportKeywordShadow(t *testing.T) {
	_, errs := aliasesOf(t, `type string = number`)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "shadows a builtin") {
		t.Fatalf("unexpected errors: %+v", errs)
	}
}

func TestFromExprAliasFallsBackToParseWithoutTable(t *testing.T) {
	prog, err := parser.Parse(`type Slug = string`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	alias := prog.Decls[0].Type
	// nil aliases map: the target `string` still resolves via Parse.
	got, ok := types.FromExprAlias(alias.Type, nil)
	if !ok || !got.Equal(types.String) {
		t.Fatalf("FromExprAlias(string, nil) = %s, %v", got, ok)
	}
}
