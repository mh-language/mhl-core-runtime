package cli_test

import (
	"strings"
	"testing"
)

func TestEnumValueIdentityAndDisplay(t *testing.T) {
	out, err := run(t, `
enum Color { Red, Green, Blue }
`+wrapStep(`
        var c = Color.Green
        log(type_of(c))
        log(c == Color.Green)
        log(c == Color.Red)
        log(c == "Green")
        log("v=${c}")
        log(json.stringify(c))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"enum\n", "true\n", "false\n", "v=Green\n", "\"Green\"\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestEnumUnknownVariantErrors(t *testing.T) {
	_, err := run(t, `
enum Color { Red, Green }
`+wrapStep(`
        log(Color.Purple)
    `))
	if err == nil || !strings.Contains(err.Error(), `enum "Color" has no variant "Purple"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnumLocalVarShadowsEnumName(t *testing.T) {
	out, err := run(t, `
enum Color { Red }
`+wrapStep(`
        var Color = {Red: "shadowed"}
        log(Color.Red)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "shadowed\n") {
		t.Errorf("a local var should shadow the enum name: %s", out)
	}
}

func TestMatchFirstEqualArmWinsAndWildcard(t *testing.T) {
	out, err := run(t, wrapStep(`
        log(match 2 { 1 -> "one" 2 -> "two" 2 -> "again" _ -> "many" })
        log(match 9 { 1 -> "one" _ -> "many" })
        log(match "b" { "a" -> 1 "b" -> 2 })
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "two\n") || !strings.Contains(out, "many\n") || !strings.Contains(out, "2\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestMatchNoArmMatchesErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        log(match 5 { 1 -> "a" 2 -> "b" })
    `))
	if err == nil || !strings.Contains(err.Error(), "match: no arm matched value 5") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnumAsToolParamType(t *testing.T) {
	_, err := run(t, `
enum Status { Draft, Live }
tool T { label(s: Status): string -> match s { Status.Draft -> "d" Status.Live -> "l" } }
`+wrapStep(`
        log(T.label("Draft"))
    `))
	if err == nil || !strings.Contains(err.Error(), `parameter "s" must be Status, got string`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
