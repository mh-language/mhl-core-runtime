package interpreter

import (
	"fmt"
	"net/http"

	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

// inProcessHost is the Bloc A extension.HostContext implementation: it wires
// straight into the runtime's own auth resolver and a shared HTTP transport.
// Bloc B's external host will provide a different implementation that brokers
// the same calls over the wire. It lives here (not in internal/extension) so
// that package stays dependency-free for lint and the LSP.
type inProcessHost struct {
	client *http.Client
	log    func(string) // receives already-formatted, not-yet-redacted lines; nil = discard
}

// ResolveSecret delegates to internal/features/auth, matching the resolution
// and redaction behaviour the interpreter already applies to `env(...)` in an
// extension declaration's configuration.
func (h *inProcessHost) ResolveSecret(ref string) (string, error) {
	return auth.Resolve(ref)
}

// HTTPClient returns the shared client, or http.DefaultClient when none was
// configured.
func (h *inProcessHost) HTTPClient() *http.Client {
	if h == nil || h.client == nil {
		return http.DefaultClient
	}
	return h.client
}

// Logf formats the line, redacts any known secret substrings, and forwards it
// to the configured sink.
func (h *inProcessHost) Logf(format string, args ...any) {
	if h == nil || h.log == nil {
		return
	}
	h.log(auth.Redact(fmt.Sprintf(format, args...)))
}

// Redact masks known secret substrings via the runtime's auth machinery.
func (h *inProcessHost) Redact(s string) string { return auth.Redact(s) }
