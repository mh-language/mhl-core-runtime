package cli_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestClaudeJSONSchemaStreamResponseShape pins down the real `claude -p ...
// --json-schema '<schema>' --output-format stream-json` response shape,
// captured from an actual run (testdata/claude_json_schema_stream_response.ndjson).
// It exists because MHL's runAgent (internal/engine/interpreter/agent.go)
// currently returns this whole multi-line NDJSON blob verbatim as `.run()`'s
// response — there is no JSON parsing in the runtime yet — so before adding
// one, this locks in exactly what it has to parse: six NDJSON lines ending
// in a `type: "result"` event whose `structured_output` field carries the
// schema-validated value. If a future `claude` release changes this shape,
// this test breaks first, not the (not yet written) MHL-side extraction.
func TestClaudeJSONSchemaStreamResponseShape(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude_json_schema_stream_response.ndjson")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")

	wantTypes := []string{"system", "assistant", "assistant", "user", "rate_limit_event", "result"}
	if len(lines) != len(wantTypes) {
		t.Fatalf("expected %d NDJSON lines, got %d", len(wantTypes), len(lines))
	}

	var events []map[string]any
	for i, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}
		if event["type"] != wantTypes[i] {
			t.Errorf("line %d: type = %v, want %q", i, event["type"], wantTypes[i])
		}
		events = append(events, event)
	}

	// The model's structured answer is carried as a "tool_use" call to a
	// synthetic "StructuredOutput" tool — not as raw JSON text in an
	// assistant content block, the way the Messages API's
	// output_config.format puts it directly in the response text.
	toolUse := events[2]["message"].(map[string]any)["content"].([]any)[0].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["name"] != "StructuredOutput" {
		t.Fatalf("expected a StructuredOutput tool_use on line 2, got: %+v", toolUse)
	}

	// The final "result" event duplicates the validated value in two
	// places: `result` (a JSON-encoded string) and `structured_output` (the
	// already-decoded object) — both must agree with the schema-validated
	// tool_use.input from line 2.
	result := events[5]
	wantMessage := "Pong! What can I help you with?"

	structuredOutput, ok := result["structured_output"].(map[string]any)
	if !ok {
		t.Fatalf("result.structured_output missing or not an object: %+v", result["structured_output"])
	}
	if structuredOutput["message"] != wantMessage {
		t.Errorf("result.structured_output.message = %v, want %q", structuredOutput["message"], wantMessage)
	}

	var resultField map[string]any
	if err := json.Unmarshal([]byte(result["result"].(string)), &resultField); err != nil {
		t.Fatalf("result.result is not valid JSON: %v", err)
	}
	if resultField["message"] != wantMessage {
		t.Errorf("result.result.message = %v, want %q", resultField["message"], wantMessage)
	}

	if toolUse["input"].(map[string]any)["message"] != wantMessage {
		t.Errorf("tool_use.input.message = %v, want %q", toolUse["input"].(map[string]any)["message"], wantMessage)
	}
}
