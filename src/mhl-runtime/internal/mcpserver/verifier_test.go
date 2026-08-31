package mcpserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(hdr map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestStaticTokenVerifier(t *testing.T) {
	// No token → everything passes, principal "".
	v := newVerifier(HTTPConfig{})
	if p, err := v.Verify(req(nil)); err != nil || p != "" {
		t.Fatalf("open server: p=%q err=%v", p, err)
	}

	v = newVerifier(HTTPConfig{Token: "s3cr3t"})
	if _, err := v.Verify(req(nil)); !errors.Is(err, errUnauthorized) {
		t.Errorf("missing token: err=%v, want errUnauthorized", err)
	}
	if _, err := v.Verify(req(map[string]string{"Authorization": "Bearer wrong"})); !errors.Is(err, errUnauthorized) {
		t.Errorf("wrong token: err=%v", err)
	}
	p, err := v.Verify(req(map[string]string{"Authorization": "Bearer s3cr3t"}))
	if err != nil || p != "" {
		t.Errorf("good token: p=%q err=%v", p, err)
	}
}

func TestTrustedHeaderVerifier(t *testing.T) {
	v := newVerifier(HTTPConfig{Token: "gw", PrincipalHeader: "X-Mhl-Principal"})

	// Header alone is not enough — the gateway↔mhl bearer must pass first.
	if _, err := v.Verify(req(map[string]string{"X-Mhl-Principal": "alice"})); !errors.Is(err, errUnauthorized) {
		t.Fatalf("header without bearer: err=%v, want errUnauthorized", err)
	}

	p, err := v.Verify(req(map[string]string{
		"Authorization":   "Bearer gw",
		"X-Mhl-Principal": "  alice@acme.com  ",
	}))
	if err != nil || p != "alice@acme.com" {
		t.Errorf("principal = %q err=%v, want alice@acme.com", p, err)
	}

	// Bearer ok, no header → authenticated but anonymous.
	if p, err := v.Verify(req(map[string]string{"Authorization": "Bearer gw"})); err != nil || p != "" {
		t.Errorf("no header: p=%q err=%v", p, err)
	}
}

func TestOwnerForVsSession(t *testing.T) {
	if ownerFor("") != "" {
		t.Error("empty principal must yield anonymous owner")
	}
	a, b := ownerFor("alice"), ownerFor("alice")
	if a != b || a == ownerFor("bob") {
		t.Error("ownerFor not a stable per-principal hash")
	}
	if string(a) == "alice" {
		t.Error("owner must be hashed, not the raw principal")
	}
	// Domain separation: a principal and a session id that happen to be equal
	// must not collide.
	if ownerFor("x") == ownerFromSession("x") {
		t.Error("ownerFor and ownerFromSession collided")
	}
}
