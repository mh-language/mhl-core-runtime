package ast_test

import (
	"reflect"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

func credRefsOf(t *testing.T, exprSrc string) []string {
	t.Helper()
	e, err := parser.ParseExpr(exprSrc)
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", exprSrc, err)
	}
	return ast.CredentialRefs(e)
}

func TestCredentialRefs(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{`"plain string"`, nil},
		{`env("TOKEN")`, []string{`env("TOKEN")`}},
		{`"Bearer " + env("GITHUB_TOKEN")`, []string{`env("GITHUB_TOKEN")`}},
		{`{ "Authorization": "Bearer " + env("A2A_TOKEN"), "X-Trace": "on" }`, []string{`env("A2A_TOKEN")`}},
		{`["--token", env("K1"), "--other", env("K2")]`, []string{`env("K1")`, `env("K2")`}},
		{`env("DUP") + "/" + env("DUP")`, []string{`env("DUP")`}},
		{`vault("db/password")`, []string{`vault("db/password")`}},
		{`(env("NESTED"))`, []string{`env("NESTED")`}},
		// MHL-Melhorias.md #1 follow-up: a reference reachable only through
		// one branch of if/match is still a real reference — hiding
		// env("TOKEN") behind `if (cond) env("TOKEN") else "x"` used to
		// silently skip both the fail-closed check and redaction.
		{`if (true) env("IF_THEN") else "x"`, []string{`env("IF_THEN")`}},
		{`if (true) "x" else env("IF_ELSE")`, []string{`env("IF_ELSE")`}},
		{`if (env("IF_COND") == "1") "x" else "y"`, []string{`env("IF_COND")`}},
		{`match status { Status.A -> env("MATCH_ARM") _ -> "x" }`, []string{`env("MATCH_ARM")`}},
		{`match env("MATCH_SUBJECT") { _ -> "x" }`, []string{`env("MATCH_SUBJECT")`}},
		// The 2-argument form, env(name, default), is the declarative way
		// to opt a reference *out* of credential treatment on purpose — it
		// must stay invisible to this scan regardless of where it appears.
		{`env("WITH_DEFAULT", "fallback")`, nil},
		{`if (true) env("WITH_DEFAULT", "fallback") else "x"`, nil},
	}
	for _, c := range cases {
		got := credRefsOf(t, c.src)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("CredentialRefs(%q) = %#v, want %#v", c.src, got, c.want)
		}
	}
}
