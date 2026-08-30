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
