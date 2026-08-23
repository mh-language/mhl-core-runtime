package nativeops_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/features/nativeops"
)

func TestPostSendsHeadersAndJSONBody(t *testing.T) {
	var gotBody map[string]any
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	result, err := nativeops.Post(context.Background(), srv.URL, map[string]string{"X-Custom": "abc"}, map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if result["status"] != 201.0 {
		t.Errorf("status = %v, want 201", result["status"])
	}
	if result["body"] != `{"ok":true}` {
		t.Errorf("body = %v", result["body"])
	}
	if gotHeader != "abc" {
		t.Errorf("X-Custom header = %q", gotHeader)
	}
	if gotBody["text"] != "hi" {
		t.Errorf("request body text = %v", gotBody["text"])
	}
}

// TestPostNonSuccessStatusIsNotAnError mirrors cmd.exec's exit_code
// philosophy: a non-2xx response is a normal, inspectable outcome
// (result["status"]), not a Go error.
func TestPostNonSuccessStatusIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	result, err := nativeops.Post(context.Background(), srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if result["status"] != 500.0 {
		t.Errorf("status = %v, want 500", result["status"])
	}
}

func TestPostConnectionFailureErrors(t *testing.T) {
	_, err := nativeops.Post(context.Background(), "http://127.0.0.1:1/unreachable", nil, nil)
	if err == nil {
		t.Fatal("expected an error for an unreachable host")
	}
}
