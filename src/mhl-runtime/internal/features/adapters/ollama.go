package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/features/tools"
)

// Ollama runs an agent turn against a local Ollama (or Ollama-compatible)
// inference server's non-streaming generate endpoint.
type Ollama struct {
	// HTTPClient is used for the request. If nil, a default client with a
	// 60s timeout is used.
	HTTPClient *http.Client
}

func (o Ollama) httpClient() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

type ollamaGenerateRequest struct {
	Model   string          `json:"model"`
	Prompt  string          `json:"prompt"`
	Stream  bool            `json:"stream"`
	Format  json.RawMessage `json:"format,omitempty"`
	Options *ollamaOptions  `json:"options,omitempty"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

// Run issues a single non-streaming POST {endpoint}/api/generate call and
// adapts Ollama's JSON response into a tools.Result carrying the generated
// text in Stdout, so callers can reuse the same response-handling code path
// as the local-subprocess adapter. temperature is omitted from the request
// entirely when nil (the agent declared no temperature property). schemaText
// is Ollama's structured-output mechanism: a JSON Schema document (as
// accepted by /api/generate's own "format" field) that constrains the
// model's response to match it — omitted entirely when "" (the agent's
// `.run()` call supplied no `schema:` argument). Unlike a CLI agent's
// "${schema}" args placeholder, this always goes out as real embedded JSON,
// not a string, so a malformed schemaText fails at the json.Marshal below
// rather than being silently sent as a broken literal.
func (o Ollama) Run(ctx context.Context, endpoint, model, promptText string, temperature *float64, schemaText string) (tools.Result, error) {
	reqBody := ollamaGenerateRequest{Model: model, Prompt: promptText, Stream: false}
	if temperature != nil {
		reqBody.Options = &ollamaOptions{Temperature: *temperature}
	}
	if schemaText != "" {
		reqBody.Format = json.RawMessage(schemaText)
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return tools.Result{}, fmt.Errorf("ollama: encoding request: %w", err)
	}

	url := strings.TrimSuffix(endpoint, "/") + "/api/generate"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return tools.Result{}, fmt.Errorf("ollama: building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient().Do(httpReq)
	if err != nil {
		return tools.Result{}, fmt.Errorf("ollama: request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.Result{}, fmt.Errorf("ollama: reading response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(body))
		if detail != "" {
			return tools.Result{}, fmt.Errorf("ollama: HTTP %s: %s", resp.Status, detail)
		}
		return tools.Result{}, fmt.Errorf("ollama: HTTP %s", resp.Status)
	}

	var decoded ollamaGenerateResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return tools.Result{}, fmt.Errorf("ollama: malformed JSON response: %w", err)
	}
	if strings.TrimSpace(decoded.Response) == "" {
		return tools.Result{}, fmt.Errorf("ollama: empty response field")
	}

	return tools.Result{Stdout: decoded.Response, ExitCode: 0}, nil
}
