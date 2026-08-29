package cli_test

import (
	"strings"
	"testing"
)

func TestTypeAliasResolvesToTarget(t *testing.T) {
	out, err := run(t, `
type Slug = string
tool T {
    make(s: Slug): Slug -> "slug:" + s
}
`+wrapStep(`
        log(T.make("hello"))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "slug:hello\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestTypeAliasEnforcedLikeItsTarget(t *testing.T) {
	_, err := run(t, `
type Slug = string
tool T { make(s: Slug): Slug -> s }
`+wrapStep(`
        log(T.make(42))
    `))
	if err == nil || !strings.Contains(err.Error(), `parameter "s" must be string, got number`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTypeAliasToObjectShape(t *testing.T) {
	_, err := run(t, `
type Point = { x: number, y: number }
tool T { origin(): Point -> { x: 0 } }
`+wrapStep(`
        log(T.origin())
    `))
	if err == nil || !strings.Contains(err.Error(), `missing field "y"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
