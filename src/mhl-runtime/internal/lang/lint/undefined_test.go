package lint_test

import (
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func messages(findings []lint.Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Message
	}
	return out
}

func TestUndefinedNames(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // substrings, one per expected finding, in order
	}{
		{
			name: "unknown receiver in a step",
			src: `
workflow W {
    step A { var x = Missing.pick() }
}`,
			want: []string{`undefined name "Missing"`},
		},
		{
			name: "unknown enum-like receiver inside a hook object literal",
			src: `
workflow W {
    step_end: (ctx: StepContext) -> {
        try {
            var d = { status: Statuz.Ok }
        } catch (e) {
            log.error("${e}")
        }
    }
    step A { log.info("x") }
}`,
			want: []string{`undefined name "Statuz"`},
		},
		{
			name: "catch without a binding, then ${e}",
			src: `
workflow W {
    step A {
        try { fail("x") } catch { log.error("falha: ${e}") }
    }
}`,
			want: []string{`undefined name "e" in "${e}"`},
		},
		{
			name: "receiver in a tool method and in a test describe",
			src: `
tool T { go(): string -> Nope.x() }
test S { describe d { var r = Gone.y() } }`,
			want: []string{`undefined name "Nope"`, `undefined name "Gone"`},
		},
		{
			name: "unparseable interpolation span",
			src: `
workflow W { step A { log.info("v=${1 +}") } }`,
			want: []string{`invalid expression in "${1 +}"`},
		},
		{
			name: "everything resolvable is quiet",
			src: `
enum Status { Ok, Bad }
tool Helper {
    var k = "v"
    twice(n: number): number -> n * 2
    use(): any -> {
        var xs = [1, 2].map((x) -> self.twice(x))
        var s = Status.Ok
        return "${k} ${xs} ${s} ${json.stringify({a: 1})} ${time.now()}"
    }
}
workflow W {
    input name: string
    var count = 0
    step A {
        var local = { a: name }
        for (var item in [1]) { log.info("${item} ${local.a} ${count} ${self.count}") }
        try { fail("x") } catch (err) { log.error("${err}") }
        var f = (p) -> {
            var inner = p
            return "${inner} ${p} ${local}"
        }
        log.info("${env("HOME", "")} ${uuid.v7()} ${context}")
        count = Helper.twice(1)
    }
}`,
		},
		{
			name: "agent prompt interpolation is resolved by the agent's before hook, not checked",
			src: `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    before: () -> { return { fetched: "x" } }
}
workflow W { step A { var r = Echo.run(prompt: "got ${fetched}") } }`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := messages(lintSrc(t, tc.src))
			if len(got) != len(tc.want) {
				t.Fatalf("want %d finding(s) %q, got %d: %q", len(tc.want), tc.want, len(got), got)
			}
			for i, w := range tc.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("finding %d: want substring %q, got %q", i, w, got[i])
				}
			}
		})
	}
}

func TestHookParamTypeMismatch(t *testing.T) {
	got := messages(lintSrc(t, `
workflow W {
    step_end: (ctx: SessionContext) -> { log.info("x") }
    session_end: (s: SessionContext) -> { log.info("y") }
    step A { log.info("z") }
}`))
	if len(got) != 1 || !strings.Contains(got[0], "step_end hook parameter is bound to a StepContext, not a SessionContext") {
		t.Fatalf("unexpected findings: %q", got)
	}
}

func TestDestructuringIsCheckedLikeItsLongForm(t *testing.T) {
	got := messages(lintSrc(t, `
workflow W {
    var known = 0
    step A {
        const c = 1
        {c} = {c: 2}
        self.{unknown} = {unknown: 1}
        var {x} = Missing.load()
        log("${x}")
    }
}`))
	want := []string{`cannot assign to constant "c"`, "self.unknown: not a declared", `undefined name "Missing"`}
	if len(got) != len(want) {
		t.Fatalf("want %d findings, got %d: %q", len(want), len(got), got)
	}
	all := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("missing a finding containing %q in %q", w, got)
		}
	}
}

func TestEnumComparedWithStringLiteral(t *testing.T) {
	got := messages(lintSrc(t, `
enum Kind { brief, adr }
tool T { is_brief(k: Kind): bool -> k == "brief" }
workflow W {
    input kind: Kind
    input name: string = ""
    step A {
        if ("x" != kind) log("1")
        if (name == "brief") log("2")
        if (kind == Kind.adr) log("3")
    }
}`))
	want := []string{
		`k is an enum Kind and never equals the string "brief" — compare with Kind.brief`,
		`kind is an enum Kind and never equals the string "x", which is not one of its variants (brief | adr)`,
	}
	if len(got) != len(want) {
		t.Fatalf("want %d findings, got %d: %q", len(want), len(got), got)
	}
	all := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("missing %q in %q", w, got)
		}
	}
}

// A test may assert an always-false comparison on purpose.
func TestEnumStringCompareIsNotFlaggedInTests(t *testing.T) {
	got := messages(lintSrc(t, `
enum Status { Published }
test T { describe d {
    var s = Status.Published
    is_false(s == "Published")
} }`))
	if len(got) != 0 {
		t.Fatalf("unexpected findings: %q", got)
	}
}
