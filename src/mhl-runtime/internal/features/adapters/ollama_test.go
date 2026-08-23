package adapters_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/features/adapters"
)

func TestOllamaRunHappyPath(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "hello from ollama"})
	}))
	defer srv.Close()

	temp := 0.2
	result, err := adapters.Ollama{}.Run(context.Background(), srv.URL, "qwen2.5-coder", "hi", &temp, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Stdout != "hello from ollama" {
		t.Errorf("Stdout = %q", result.Stdout)
	}

	if gotBody["model"] != "qwen2.5-coder" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if gotBody["prompt"] != "hi" {
		t.Errorf("prompt = %v", gotBody["prompt"])
	}
	if gotBody["stream"] != false {
		t.Errorf("stream = %v", gotBody["stream"])
	}
	options, ok := gotBody["options"].(map[string]any)
	if !ok {
		t.Fatalf("options missing or wrong type: %v", gotBody["options"])
	}
	if options["temperature"] != 0.2 {
		t.Errorf("temperature = %v", options["temperature"])
	}
}

func TestOllamaRunSendsSchemaAsFormat(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": `{"message": "hi"}`})
	}))
	defer srv.Close()

	schema := `{"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"]}`
	_, err := adapters.Ollama{}.Run(context.Background(), srv.URL, "qwen2.5-coder", "hi", nil, schema)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	format, ok := gotBody["format"].(map[string]any)
	if !ok {
		t.Fatalf("format missing or wrong type: %v", gotBody["format"])
	}
	if format["type"] != "object" {
		t.Errorf("format.type = %v", format["type"])
	}
}

func TestOllamaRunOmitsFormatWhenSchemaEmpty(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "ok"})
	}))
	defer srv.Close()

	if _, err := (adapters.Ollama{}).Run(context.Background(), srv.URL, "m", "hi", nil, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, present := gotBody["format"]; present {
		t.Errorf("expected no 'format' key in the request body, got: %v", gotBody)
	}
}

func TestOllamaRunOmitsOptionsWhenTemperatureNil(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "ok"})
	}))
	defer srv.Close()

	_, err := adapters.Ollama{}.Run(context.Background(), srv.URL, "qwen2.5-coder", "hi", nil, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, present := gotBody["options"]; present {
		t.Errorf("expected no 'options' key in the request body, got: %v", gotBody)
	}
}

func TestOllamaRunNonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := adapters.Ollama{}.Run(context.Background(), srv.URL, "missing-model", "hi", nil, "")
	if err == nil {
		t.Fatal("expected an error for a non-2xx response")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOllamaRunMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := adapters.Ollama{}.Run(context.Background(), srv.URL, "m", "hi", nil, "")
	if err == nil {
		t.Fatal("expected an error for a malformed JSON response")
	}
	if !strings.Contains(err.Error(), "malformed JSON") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOllamaRunEmptyResponseField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"response": ""})
	}))
	defer srv.Close()

	_, err := adapters.Ollama{}.Run(context.Background(), srv.URL, "m", "hi", nil, "")
	if err == nil {
		t.Fatal("expected an error for an empty response field")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOllamaRunConnectionFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // closed before use: guarantees a connection failure

	_, err := adapters.Ollama{}.Run(context.Background(), srv.URL, "m", "hi", nil, "")
	if err == nil {
		t.Fatal("expected an error for a connection failure")
	}
	if !strings.Contains(err.Error(), "ollama: request to") {
		t.Errorf("unexpected error: %v", err)
	}
}
