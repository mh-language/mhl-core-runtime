package cli_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// runEvents runs src with --format jsonl and decodes every stdout line.
func runEvents(t *testing.T, src string, extra ...string) ([]map[string]any, error) {
	t.Helper()
	chdirTemp(t)
	if err := os.WriteFile("p.mh", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	runErr := cli.Run(append([]string{"run", "p.mh", "--format", "jsonl"}, extra...), &buf)
	return decodeEvents(t, buf.String()), runErr
}

func decodeEvents(t *testing.T, out string) []map[string]any {
	t.Helper()
	var evs []map[string]any
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		var ev map[string]any
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("line is not JSON: %v\n%s", err, sc.Text())
		}
		evs = append(evs, ev)
	}
	return evs
}

// steps renders the step_start/step_end events as "start:X" / "end:X:status".
func stepTrace(evs []map[string]any) []string {
	var out []string
	for _, ev := range evs {
		switch ev["type"] {
		case "step_start":
			out = append(out, "start:"+ev["step"].(string))
		case "step_end":
			s := "end:" + ev["step"].(string) + ":" + ev["status"].(string)
			if tgt, ok := ev["target"].(string); ok {
				s += ">" + tgt
			}
			out = append(out, s)
		}
	}
	return out
}

func TestRunFormatJSONLStreamsStepEvents(t *testing.T) {
	evs, err := runEvents(t, `
workflow W {
    var n = 0
    step A {
        n += 1
        log("visit " + n)
        if (n < 2) goto A
    }
    step Gate { pause("approve") }
}
`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if evs[0]["type"] != "log" && evs[0]["type"] != "run_start" {
		t.Fatalf("first event = %v", evs[0])
	}
	var start map[string]any
	for _, ev := range evs {
		if ev["type"] == "run_start" {
			start = ev
		}
	}
	if start == nil || start["pipeline"] != "W" || len(start["steps"].([]any)) != 2 {
		t.Fatalf("run_start = %v", start)
	}
	want := "start:A end:A:goto>A start:A end:A:ok start:Gate end:Gate:pause"
	if got := strings.Join(stepTrace(evs), " "); got != want {
		t.Errorf("trace = %s\nwant    %s", got, want)
	}
	var logs []string
	for _, ev := range evs {
		if ev["type"] == "log" {
			logs = append(logs, ev["text"].(string))
		}
	}
	if !strings.Contains(strings.Join(logs, "\n"), "visit 2") {
		t.Errorf("log events = %q", logs)
	}
	last := evs[len(evs)-1]
	if last["type"] != "result" || last["ok"] != true || last["paused"] != true || last["pause_reason"] != "approve" {
		t.Errorf("result = %v", last)
	}
	if _, hasLog := last["log"]; hasLog {
		t.Errorf("result must not repeat the log: %v", last)
	}
}

func TestRunFormatJSONLFailure(t *testing.T) {
	evs, err := runEvents(t, `
pipeline P {
    step Boom { fail("nope") }
}
`)
	if err == nil {
		t.Fatal("want a run error")
	}
	trace := stepTrace(evs)
	if len(trace) != 2 || trace[1] != "end:Boom:failed" {
		t.Fatalf("trace = %v", trace)
	}
	for _, ev := range evs {
		if ev["type"] == "step_end" && !strings.Contains(ev["error"].(string), "nope") {
			t.Errorf("step_end error = %v", ev["error"])
		}
	}
	last := evs[len(evs)-1]
	if last["type"] != "result" || last["ok"] != false || last["kind"] != "step_failed" || last["step"] != "Boom" {
		t.Errorf("result = %v", last)
	}
}

func TestRunFormatJSONLRejectsDryRun(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("p.mh", []byte("pipeline P { step A { } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cli.Run([]string{"run", "p.mh", "--dry-run", "--format", "jsonl"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--dry-run") {
		t.Fatalf("err = %v", err)
	}
}

// --workflow picks which declared workflow runs; without it the first entry
// point runs.
func TestRunWorkflowFlagSelectsTheWorkflow(t *testing.T) {
	src := `
workflow First { step A { log("first") } }
workflow Second {
    input who: string
    step B { log("second " + who) }
}
`
	evs, err := runEvents(t, src, "--workflow", "Second", "--input", "who=x")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Join(stepTrace(evs), " "); got != "start:B end:B:ok" {
		t.Errorf("trace = %s", got)
	}
	evs, err = runEvents(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Join(stepTrace(evs), " "); got != "start:A end:A:ok" {
		t.Errorf("default trace = %s", got)
	}
	var out bytes.Buffer
	if err := cli.Run([]string{"run", "p.mh", "--dry-run", "--format", "json", "--workflow", "Second"}, &out); err == nil {
		t.Errorf("dry-run without the required input should fail: %s", out.String())
	}
	if !strings.Contains(out.String(), `"pipeline": "Second"`) || !strings.Contains(out.String(), `"who"`) {
		t.Errorf("dry-run did not inspect Second: %s", out.String())
	}
	if _, err := runEvents(t, src, "--workflow", "Nope"); err == nil {
		t.Error("an unknown --workflow must fail")
	}
}
