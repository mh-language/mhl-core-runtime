package types_test

import (
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

type fakeEnum struct{ name string }

func (f fakeEnum) EnumName() string { return f.name }

func TestEnumTypeEqualAndString(t *testing.T) {
	a := types.EnumType("Status")
	if !a.Equal(types.EnumType("Status")) {
		t.Error("same enum should be Equal")
	}
	if a.Equal(types.EnumType("Other")) {
		t.Error("different enum names must not be Equal")
	}
	if a.Equal(types.String) {
		t.Error("enum must not equal string")
	}
	if a.String() != "Status" {
		t.Errorf("String() = %q", a.String())
	}
}

func TestEnumCheckAcceptsMatchingCarrierOnly(t *testing.T) {
	declared := types.EnumType("Status")

	if err := types.Check("x", declared, fakeEnum{"Status"}); err != nil {
		t.Errorf("matching enum value should pass: %v", err)
	}
	if err := types.Check("x", declared, fakeEnum{"Other"}); err == nil ||
		!strings.Contains(err.Error(), "must be Status, got Other") {
		t.Errorf("wrong enum: %v", err)
	}
	if err := types.Check("x", declared, "Draft"); err == nil ||
		!strings.Contains(err.Error(), "must be Status, got string") {
		t.Errorf("plain string must be rejected: %v", err)
	}
}

func TestAliasesRegisterEnumNames(t *testing.T) {
	prog, err := parser.Parse(`
enum Status { Draft, Live }
type Alias = Status
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m, errs := types.Aliases(prog)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if got := m["Status"]; !got.Equal(types.EnumType("Status")) {
		t.Errorf("Status = %s", got)
	}
	if got := m["Alias"]; !got.Equal(types.EnumType("Status")) {
		t.Errorf("Alias should resolve to the Status enum, got %s", got)
	}
}
