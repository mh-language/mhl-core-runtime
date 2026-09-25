package cli_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Plain objects/arrays are values; `ref { ... }` objects are shared and keep
// their identity across a checkpoint. These tests run real programs through
// `mhl run`.

func TestValueSemanticsNeverAliases(t *testing.T) {
	cases := map[string]string{
		"assignment": `
        var a = { x: 1 }
        var b = a
        b["x"] = 2
        log("a.x=" + json.stringify(a.x))`,
		"nested part": `
        var a = { inner: { x: 1 } }
        var part = a.inner
        part["x"] = 2
        log("a.x=" + json.stringify(a.inner.x))`,
		"inside a literal": `
        var a = { x: 1 }
        var box = { held: a }
        box["held"]["x"] = 2
        log("a.x=" + json.stringify(a.x))`,
		"lambda argument": `
        var a = { x: 1 }
        var f = (o) -> { o["x"] = 2 }
        f(a)
        log("a.x=" + json.stringify(a.x))`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := run(t, wrapStep(body))
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if !strings.Contains(out, "a.x=1") {
				t.Fatalf("a plain object must not change through another name:\n%s", out)
			}
		})
	}
}

func TestRefSharesAcrossNamesPartsAndCalls(t *testing.T) {
	out, err := run(t, wrapStep(`
        var a = ref { x: 1 }
        var b = a
        b["x"] = 2
        var box = { held: a }
        box["held"]["x"] = 3
        var f = (o) -> { o["x"] = 4 }
        f(a)
        log("a.x=" + json.stringify(a.x) + " same=" + json.stringify(a == b))
        log("other=" + json.stringify(a == ref { x: 4 }))
        log("json=" + json.stringify(a))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"a.x=4 same=true", "other=false", `json={"x":4}`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRefExpressionForms(t *testing.T) {
	out, err := run(t, wrapStep(`
        var plain = { x: 1 }
        var r = ref (plain)
        r["x"] = 2
        log("plain.x=" + json.stringify(plain.x) + " r.x=" + json.stringify(r.x))
        var ref = 5
        log("ident=" + json.stringify(ref + 1))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "plain.x=1 r.x=2") || !strings.Contains(out, "ident=6") {
		t.Fatalf("ref (expr) copies its source; `ref` stays usable as a name:\n%s", out)
	}

	for body, want := range map[string]string{
		`var r = ref ("text")`:                 "ref: needs an object, got string",
		"var a = ref { x: 1 }\n var b = ref (a)": "already a ref object",
	} {
		if _, err := run(t, wrapStep(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", body, want, err)
		}
	}
}

// The same program must give the same result whether or not it was stopped
// and resumed in the middle — for plain values and refs alike.
func TestResumeIsEquivalentToADirectRun(t *testing.T) {
	src := `
workflow P {
    input go: string
    var a = { x: 1 }
    var b = { x: 0 }
    var r = ref { x: 1 }
    var s = { x: 0 }
    step Link { b = a
        s = r }
    step Gate { if (go != "yes") { pause("wait") } }
    step Mutate {
        b["x"] = 2
        s["x"] = 2
    }
    output: { plain: a.x, shared: r.x, same: r == s }
}
`
	want := map[string]any{"plain": 1.0, "shared": 2.0, "same": true}

	direct, err := runJSONOut(t, src, "--input", "go=yes")
	if err != nil || !reflect.DeepEqual(direct["vars"], want) {
		t.Fatalf("direct run: err=%v vars=%#v, want %#v", err, direct["vars"], want)
	}

	run := runJSONFiles(t, map[string]string{"main.mh": src})
	if _, err := run("main.mh", "--session", "eq", "--input", "go=no"); err != nil {
		t.Fatalf("first leg: %v", err)
	}
	resumed, err := run("main.mh", "--session", "eq", "--resume", "--input", "go=yes")
	if err != nil || !reflect.DeepEqual(resumed["vars"], want) {
		t.Fatalf("resumed run: err=%v vars=%#v, want %#v (same as the direct run)", err, resumed["vars"], want)
	}
}

func TestRefCycleSurvivesResumeButHasNoJSONForm(t *testing.T) {
	run := runJSONFiles(t, map[string]string{"main.mh": `
workflow P {
    input go: string
    var node = ref { name: "n" }
    step Link { node["self"] = node }
    step Gate { if (go != "yes") { pause("wait") } }
    step Check { log("CYCLE " + json.stringify(node.self.self.name) + " " + json.stringify(node.self == node)) }
    output: { name: node.name }
}
`})
	if _, err := run("main.mh", "--session", "cyc", "--input", "go=no"); err != nil {
		t.Fatalf("first leg: %v", err)
	}
	obj, err := run("main.mh", "--session", "cyc", "--resume", "--input", "go=yes")
	if err != nil {
		t.Fatalf("resume: %v (%#v)", err, obj)
	}
	if log, _ := obj["log"].(string); !strings.Contains(log, `CYCLE "n" true`) {
		t.Fatalf("the cycle must come back as the same object:\n%s", log)
	}

	obj, err = runJSONOut(t, `
pipeline P {
    var node = ref { name: "n" }
    step Link { node["self"] = node }
}
`)
	if err == nil || !strings.Contains(obj["error"].(string), "contains itself") {
		t.Fatalf("a cyclic ref in the result has no JSON form: err=%v obj=%#v", err, obj)
	}
}

func TestParallelMergesRefFieldsByIdentity(t *testing.T) {
	obj, err := runJSONOut(t, `
pipeline P {
    var r = ref { a: 0, b: 0 }
    var alias = { x: 0 }
    step Link { alias = r }
    parallel Both {
        step A { r["a"] = 1 }
        step B { alias["b"] = 2 }
    }
    output: { a: r.a, b: r.b, same: r == alias }
}
`)
	if err != nil {
		t.Fatalf("run: %v (%#v)", err, obj)
	}
	if want := map[string]any{"a": 1.0, "b": 2.0, "same": true}; !reflect.DeepEqual(obj["vars"], want) {
		t.Fatalf("vars = %#v, want %#v (different fields of one ref both land, identity kept)", obj["vars"], want)
	}

	obj, err = runJSONOut(t, `
pipeline P {
    var r = ref { a: 0 }
    parallel Both {
        step A { r["a"] = 1 }
        step B { r["a"] = 2 }
    }
}
`)
	if err == nil || !strings.Contains(obj["error"].(string), `both changed field "a" of the same ref object`) {
		t.Fatalf("the same field set to different values is a conflict: err=%v obj=%#v", err, obj)
	}
}

func TestRefLeavesTheInterpreterAsPlainData(t *testing.T) {
	run := runJSONFiles(t, map[string]string{"main.mh": `
pipeline P {
    mem saved = {}
    var r = ref { n: 1 }
    step S {
        saved = r
        r["n"] = 2
        fs.write("out.json", json.stringify(r))
    }
    output: { r: r, saved: saved }
}
`})
	obj, err := run("main.mh")
	if err != nil {
		t.Fatalf("run: %v (%#v)", err, obj)
	}
	want := map[string]any{"r": map[string]any{"n": 2.0}, "saved": map[string]any{"n": 1.0}}
	if !reflect.DeepEqual(obj["vars"], want) {
		t.Fatalf("vars = %#v, want %#v (result is plain; mem stored a plain snapshot)", obj["vars"], want)
	}
}

func TestSecretInsideARefIsRedactedInTheResult(t *testing.T) {
	t.Setenv("MHL_REF_TEST_SECRET", "s3cr3t-value-123")
	obj, err := runJSONOut(t, `
pipeline P {
    var r = ref { token: "" }
    step S { r["token"] = env("MHL_REF_TEST_SECRET") }
}
`)
	if err != nil {
		t.Fatalf("run: %v (%#v)", err, obj)
	}
	if strings.Contains(mustJSON(t, obj), "s3cr3t-value-123") {
		t.Fatalf("a secret held in a ref leaked into the result: %#v", obj["vars"])
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
