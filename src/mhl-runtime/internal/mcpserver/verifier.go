package mcpserver

import (
	"errors"
	"net/http"
	"strings"
)

// errUnauthorized is what a TokenVerifier returns for a request that fails the
// bearer check; handleMCP maps it to 401. `X-Mhl-Principal` is the
// conventional --principal-header name.
var errUnauthorized = errors.New("unauthorized")

// TokenVerifier extracts the caller's principal from an HTTP request. It never
// decides whether the caller may do something — that is the job of the mesh /
// gateway authorizer (Phase 2's deliberate split). A "" principal is allowed:
// it means "no verified identity", and run ownership then falls back to the
// Phase-0 per-session hash (ownerFromSession).
type TokenVerifier interface {
	Verify(r *http.Request) (principal string, err error)
}

// staticToken is the historical guard: an `Authorization: Bearer <token>`
// equality check, no principal. An empty token disables auth entirely (any
// request passes, principal "").
type staticToken struct{ token string }

func (s staticToken) Verify(r *http.Request) (string, error) {
	if s.token == "" {
		return "", nil
	}
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		return "", errUnauthorized
	}
	return "", nil
}

// trustedHeader takes the principal from a header an upstream set after it
// authenticated the caller (an API Gateway authorizer, Envoy/Istio). The
// request MUST still pass the shared gateway↔mhl bearer check first — without
// it a client could set the header and impersonate anyone, so serve.go
// refuses --principal-header without --token.
type trustedHeader struct {
	header string
	inner  staticToken
}

func (t trustedHeader) Verify(r *http.Request) (string, error) {
	if _, err := t.inner.Verify(r); err != nil {
		return "", err
	}
	return strings.TrimSpace(r.Header.Get(t.header)), nil
}

// newVerifier builds the verifier for cfg: trustedHeader when PrincipalHeader
// is set (it wraps the same bearer check), otherwise the plain staticToken.
func newVerifier(cfg HTTPConfig) TokenVerifier {
	st := staticToken{token: cfg.Token}
	if cfg.PrincipalHeader != "" {
		return trustedHeader{header: cfg.PrincipalHeader, inner: st}
	}
	return st
}
