package nativeops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultHTTPTimeout mirrors internal/mcp.NewClient's default — a stateless
// HTTP call needs some bound, and this package has no per-call config
// surface for it (a `tool` method body can't set one), so a single sane
// default is used everywhere.
const defaultHTTPTimeout = 30 * time.Second

// Post issues an HTTP POST to url with the given headers and a JSON-encoded
// body (any MHL value — string/number/bool/array/object), and returns the
// response as {"status": float64, "body": string}. A non-2xx response is
// not itself an error — same philosophy as Exec's exit_code — only a
// genuine failure to complete the request (DNS, connection refused,
// timeout) errors, so a .mh pipeline can branch on response.status.
func Post(ctx context.Context, url string, headers map[string]string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("http.post %q: encoding body: %w", url, err)
		}
		reader = bytes.NewReader(raw)
	}

	ctx, cancel := context.WithTimeout(ctx, defaultHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reader)
	if err != nil {
		return nil, fmt.Errorf("http.post %q: building request: %w", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: defaultHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http.post %q: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http.post %q: reading response: %w", url, err)
	}
	return map[string]any{
		"status": float64(resp.StatusCode),
		"body":   string(respBody),
	}, nil
}
