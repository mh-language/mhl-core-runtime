package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

func runJSONOut(t *testing.T, src string, extra ...string) (map[string]any, error) {
	t.Helper()
	chdirTemp(t)
	if err := os.WriteFile("p.mh", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	args := append([]string{"run", "p.mh", "--format", "json"}, extra...)
	runErr := cli.Run(args, &buf)
	var obj map[string]any
	if err := json.Unmarshal(buf.Bytes(), &obj); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v\n%s", err, buf.String())
	}
	return obj, runErr
}

func TestRunFormatJSONSuccess(t *testing.T) {
	obj, err := runJSONOut(t, `
pipeline Greet {
    input name: string
    var message = ""
    step Build { message = "hi " + name }
}
`, "--input", "name=ana")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if obj["ok"] != true {
		t.Fatalf("ok = %v, want true: %#v", obj["ok"], obj)
	}
	if obj["pipeline"] != "Greet" {
		t.Errorf("pipeline = %v", obj["pipeline"])
	}
	vars, _ := obj["vars"].(map[string]any)
	if vars["message"] != "hi ana" {
		t.Errorf("vars.message = %v", vars["message"])
	}
	exec, _ := obj["executed"].([]any)
	if len(exec) != 1 || exec[0] != "Build" {
		t.Errorf("executed = %v", obj["executed"])
	}
}

func TestRunFormatJSONStepFailure(t *testing.T) {
	obj, err := runJSONOut(t, `
pipeline Flow {
    step One { var x = 1 }
    step Two { fail("boom") }
}
`)
	if err == nil {
		t.Fatal("expected a non-nil error so the process exits non-zero")
	}
	if obj["ok"] != false {
		t.Fatalf("ok = %v, want false", obj["ok"])
	}
	if obj["kind"] != "step_failed" {
		t.Errorf("kind = %v, want step_failed", obj["kind"])
	}
	if obj["step"] != "Two" {
		t.Errorf("step = %v, want Two", obj["step"])
	}
	if s, _ := obj["hint"].(string); s == "" {
		t.Error("expected a non-empty hint")
	}
	if s, _ := obj["error"].(string); !strings.Contains(s, "boom") {
		t.Errorf("error = %q, want it to mention the fail() message", s)
	}
}

func TestRunFormatJSONInvalidInputs(t *testing.T) {
	obj, err := runJSONOut(t, `
pipeline NeedsInput {
    input required_id: string
    step S { var x = required_id }
}
`)
	if err == nil {
		t.Fatal("expected an error for the missing required input")
	}
	if obj["kind"] != "invalid_inputs" {
		t.Fatalf("kind = %v, want invalid_inputs: %#v", obj["kind"], obj)
	}
	missing, _ := obj["missing"].([]any)
	if len(missing) != 1 || missing[0] != "required_id" {
		t.Errorf("missing = %v, want [required_id]", obj["missing"])
	}
}

func TestRunFormatJSONCapturesLogOutOfBand(t *testing.T) {
	obj, err := runJSONOut(t, `
pipeline Chatty {
    step S { log("hello from the step") }
}
`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	logField, _ := obj["log"].(string)
	if !strings.Contains(logField, "hello from the step") {
		t.Errorf("log field = %q, want the step's log() line", logField)
	}
}

func TestRunFormatJSONRedactsSecrets(t *testing.T) {
	t.Setenv("MHL_FORMAT_SECRET_TOKEN", "super-secret-value-xyz")
	obj, err := runJSONOut(t, `
pipeline Leak {
    var t = env("MHL_FORMAT_SECRET_TOKEN")
    step S { fail("token was " + t) }
}
`)
	if err == nil {
		t.Fatal("expected the step to fail")
	}
	blob, _ := json.Marshal(obj)
	if strings.Contains(string(blob), "super-secret-value-xyz") {
		t.Fatalf("JSON output leaked a secret: %s", blob)
	}
}

func TestRunFormatRejectsUnknownValue(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("p.mh", []byte("pipeline P { step S { var x = 1 } }"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cli.Run([]string{"run", "p.mh", "--format", "yaml"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "text or json") {
		t.Fatalf("want a --format validation error, got: %v", err)
	}
}

func TestRunFormatJSONSuccessRedactsNestedSecrets(t *testing.T) {
	const secret = "synthetic-json-success-secret-8361"
	for _, projection := range []struct {
		name   string
		output string
	}{
		{"legacy", ""},
		{"explicit", "output: { scalar: scalar, object: object, array: array, public: public }"},
	} {
		t.Run(projection.name, func(t *testing.T) {
			t.Setenv("MHL_JSON_SUCCESS_SECRET", secret)
			obj, err := runJSONOut(t, `
pipeline Protected {
    var scalar = env("MHL_JSON_SUCCESS_SECRET")
    var object = { nested: { token: scalar }, count: 42 }
    var array = [scalar, { token: scalar }, "visible"]
    var public = "ordinary result"
    `+projection.output+`
    step Done { log("completed " + scalar) }
}
`)
			if err != nil || obj["ok"] != true {
				t.Fatalf("successful JSON run: err=%v, result=%v", err, obj)
			}
			blob, err := json.Marshal(obj)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(blob), secret) {
				t.Fatal("successful JSON run exposed the registered secret")
			}
			want := map[string]any{
				"scalar": "[REDACTED]",
				"object": map[string]any{"nested": map[string]any{"token": "[REDACTED]"}, "count": float64(42)},
				"array":  []any{"[REDACTED]", map[string]any{"token": "[REDACTED]"}, "visible"},
				"public": "ordinary result",
			}
			if !reflect.DeepEqual(obj["vars"], want) {
				t.Errorf("vars = %#v, want %#v", obj["vars"], want)
			}
		})
	}
}

// A run that pauses before its `output:` mapping can be evaluated is reported
// as a paused success, not a JSON error. (R6 in PLANO.md.)
func TestRunFormatJSONPauseBeforeOutputIsNotAnError(t *testing.T) {
	obj, err := runJSONOut(t, `
workflow Ingest {
    input approved: string
    checkpoint: { enabled: true, strategy: "per_step" }
    var payload = ""
    output: { parsed: json.parse(payload) }
    step Gate { if (approved != "yes") { pause("hold") } }
    step Load { payload = "{\"k\": 1}" }
}
`, "--input", "approved=no")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if obj["ok"] != true {
		t.Fatalf("ok = %v, want true (paused run is a success): %#v", obj["ok"], obj)
	}
	if obj["paused"] != true {
		t.Fatalf("paused = %v, want true: %#v", obj["paused"], obj)
	}
	if _, isErr := obj["error"]; isErr {
		t.Fatalf("paused run carried an error field: %#v", obj)
	}
}
