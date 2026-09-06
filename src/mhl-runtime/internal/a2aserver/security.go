package a2aserver

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// DefaultMaxBodyBytes bounds a single A2A JSON-RPC request body when the
// Config sets no MaxBodyBytes of its own.
const DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// Config is the operational hardening a served A2A endpoint applies —
// deliberately the same surface as `mhl serve mcp --http`: a shared bearer
// token, an optional trusted principal header (which requires the token, so a
// client cannot spoof it), and a request-body cap. A zero Config is an
// unauthenticated endpoint with the default body cap, matching the previous
// behaviour.
type Config struct {
	// Token, when non-empty, is required as `Authorization: Bearer <token>`
	// on every JSON-RPC request; a mismatch is 401.
	Token string
	// PrincipalHeader, when set, is the header an authenticated upstream
	// (API gateway, service mesh) puts the caller identity in. It is only
	// honoured alongside Token — without the shared secret the header is
	// client-spoofable — and serve.go refuses the flag combination that
	// would leave it unguarded.
	PrincipalHeader string
	// MaxBodyBytes caps a request body; DefaultMaxBodyBytes when zero.
	MaxBodyBytes int64
}

// guard applies Config to each request: DNS-rebinding Origin check, bearer
// token, body cap, and principal extraction.
type guard struct {
	token           string
	principalHeader string
	maxBody         int64
}

func newGuard(cfg Config) guard {
	mb := cfg.MaxBodyBytes
	if mb <= 0 {
		mb = DefaultMaxBodyBytes
	}
	return guard{token: cfg.Token, principalHeader: cfg.PrincipalHeader, maxBody: mb}
}

// check enforces the guard on r. It returns the caller principal ("" when no
// PrincipalHeader is configured or the header is absent) and ok=false when it
// has already written an error response.
func (g guard) check(w http.ResponseWriter, r *http.Request) (principal string, ok bool) {
	// A browser sends Origin; a non-browser client usually does not. Reject
	// any cross-origin request that is not loopback (DNS-rebinding guard).
	if o := r.Header.Get("Origin"); o != "" && !originAllowed(o) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return "", false
	}
	if g.token != "" && r.Header.Get("Authorization") != "Bearer "+g.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, g.maxBody)
	if g.principalHeader != "" {
		principal = strings.TrimSpace(r.Header.Get(g.principalHeader))
	}
	return principal, true
}

func originAllowed(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
