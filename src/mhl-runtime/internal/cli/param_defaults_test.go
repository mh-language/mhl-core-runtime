package cli_test

import (
	"strings"
	"testing"
)

func TestToolMethodDefaultParamOmitted(t *testing.T) {
	out, err := run(t, `
tool T {
    greet(name: string, greeting: string = "Hello") -> greeting + ", " + name
}
`+wrapStep(`
        log(T.greet("world"))
        log(T.greet("world", "Hi"))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "Hello, world\n") || !strings.Contains(out, "Hi, world\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolMethodDefaultParamCanReadEarlierParam(t *testing.T) {
	out, err := run(t, `
tool T {
    box(open: string, close: string = open) -> open + "x" + close
}
`+wrapStep(`
        log(T.box("["))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "[x[\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolMethodDefaultParamTypeChecked(t *testing.T) {
	_, err := run(t, `
tool T {
    n(x: number, y: number = "nope") -> x + y
}
`+wrapStep(`
        log(T.n(1))
    `))
	if err == nil || !strings.Contains(err.Error(), `parameter "y" must be number, got string`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolMethodTooManyArgsErrors(t *testing.T) {
	_, err := run(t, `
tool T {
    greet(name: string, greeting: string = "Hi") -> greeting
}
`+wrapStep(`
        log(T.greet("a", "b", "c"))
    `))
	if err == nil || !strings.Contains(err.Error(), "requires between 1 and 2 argument(s), got 3") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClosureDefaultParamOmitted(t *testing.T) {
	out, err := run(t, wrapStep(`
        var add = (a, b = 10) -> a + b
        log(add(5))
        log(add(5, 1))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "15\n") || !strings.Contains(out, "6\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestPromptDefaultParamOmitted(t *testing.T) {
	out, err := run(t, `
prompt Greet(name: string, lang: string = "en") {
    "hello ${name} (${lang})"
}
agent Echo { command: "printf" args: ["%s"] }
`+wrapStep(`
        log(Echo.run(prompt: Greet(name: "ana")))
        log(Echo.run(prompt: Greet(name: "ana", lang: "pt")))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hello ana (en)") || !strings.Contains(out, "hello ana (pt)") {
		t.Errorf("unexpected output: %s", out)
	}
}

// A non-defaulted parameter after a defaulted one is a lint error, but lint
// does not block `mhl run`; the interpreter must still fail cleanly (not
// panic) when such a method is actually called with the earlier argument
// omitted.
func TestToolMethodNonDefaultAfterDefaultRunFailsCleanly(t *testing.T) {
	_, err := run(t, `
tool T {
    bad(a: string = "x", b: string) -> a + b
}
`+wrapStep(`
        log(T.bad("only-a"))
    `))
	if err == nil || !strings.Contains(err.Error(), `missing argument for parameter "b"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
