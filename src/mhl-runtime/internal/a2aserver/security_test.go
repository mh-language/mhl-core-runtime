package a2aserver_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/a2aserver"
)

func guardedServer(t *testing.T, cfg a2aserver.Config) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "g.mh"), []byte(greet), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := a2aserver.HandlerConfig(dir, "http://test/", cfg, io.Discard)
	if err != nil {
		t.Fatalf("HandlerConfig: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func postRPC(t *testing.T, url string, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":{"id":"x"}}`))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	return resp
}

// With a token configured, a JSON-RPC request without the matching bearer is 401.
func TestA2ARejectsMissingBearer(t *testing.T) {
	ts := guardedServer(t, a2aserver.Config{Token: "s3cret"})

	resp := postRPC(t, ts.URL+"/", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no bearer: status = %d, want 401", resp.StatusCode)
	}

	resp2 := postRPC(t, ts.URL+"/", map[string]string{"Authorization": "Bearer wrong"})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong bearer: status = %d, want 401", resp2.StatusCode)
	}

	resp3 := postRPC(t, ts.URL+"/", map[string]string{"Authorization": "Bearer s3cret"})
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("correct bearer: status = %d, want 200", resp3.StatusCode)
	}
}

// The agent card stays reachable without a token (discovery is public).
func TestA2ACardIsPublic(t *testing.T) {
	ts := guardedServer(t, a2aserver.Config{Token: "s3cret"})
	resp, err := http.Get(ts.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent card status = %d, want 200", resp.StatusCode)
	}
}

// A cross-origin (non-loopback Origin) request is refused — DNS-rebinding guard.
func TestA2ARejectsForeignOrigin(t *testing.T) {
	ts := guardedServer(t, a2aserver.Config{})
	resp := postRPC(t, ts.URL+"/", map[string]string{"Origin": "https://evil.example.com"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin: status = %d, want 403", resp.StatusCode)
	}
}

// An oversized body is rejected once the cap is exceeded.
func TestA2ABodyCap(t *testing.T) {
	ts := guardedServer(t, a2aserver.Config{MaxBodyBytes: 64})
	big := `{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":{"id":"` + strings.Repeat("x", 500) + `"}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	// The body reader errors past the cap; the RPC decode then fails — either a
	// 4xx/5xx status or a JSON-RPC parse error in the body is acceptable, as
	// long as the request did not succeed.
	if resp.StatusCode == http.StatusOK {
		var sb strings.Builder
		_, _ = io.Copy(&sb, resp.Body)
		if !strings.Contains(sb.String(), "parse error") {
			t.Fatalf("oversized body was accepted: %s", sb.String())
		}
	}
}
