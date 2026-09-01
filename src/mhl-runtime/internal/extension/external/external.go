package external

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// hostName / hostVersion identify this runtime in the handshake. hostVersion
// is overwritten by the CLI at init, like mcp.ClientVersion.
var (
	hostName    = "mhl"
	hostVersion = "0"
)

// SetHostVersion lets the composition root report the real CLI version in the
// handshake.
func SetHostVersion(v string) { hostVersion = v }

// shutdownGrace bounds how long Close waits for a cooperative exit before it
// kills the child.
const shutdownGrace = 3 * time.Second

// maxRestarts caps how many times a crashed extension is respawned within one
// runtime lifetime, so a boot-looping extension fails the run instead of
// forking forever.
const maxRestarts = 3

// External adapts a persistent child process to extension.Extension. One
// process is shared by every declaration of the kinds this extension serves;
// it is started on first call, not at registration.
type External struct {
	manifest *Manifest

	mu       sync.Mutex
	host     extension.HostContext
	proc     *process
	restarts int
}

// New builds an External from a loaded manifest.
func New(m *Manifest) *External { return &External{manifest: m} }

func (e *External) ID() string      { return e.manifest.ID }
func (e *External) Version() string { return e.manifest.Version }

// Declarations comes straight from the manifest — lint and the LSP read it
// without the process ever running.
func (e *External) Declarations() []extension.DeclarationSpec {
	return e.manifest.Declares
}

// Validate does the static checks the manifest allows without I/O.
func (e *External) Validate(decl extension.Declaration) []extension.Diagnostic {
	for _, spec := range e.manifest.Declares {
		if spec.Kind != decl.Kind {
			continue
		}
		var diags []extension.Diagnostic
		known := map[string]bool{}
		for _, p := range spec.Properties {
			known[p.Name] = true
		}
		for _, p := range decl.Props {
			if len(known) > 0 && !known[p.Name] {
				diags = append(diags, extension.Diagnostic{
					Severity: extension.SeverityWarning,
					Code:     "unknown-property",
					Pos:      p.Pos,
					Message:  fmt.Sprintf("%s %q has no property %q", decl.Kind, decl.Name, p.Name),
				})
			}
		}
		return diags
	}
	return []extension.Diagnostic{{
		Severity: extension.SeverityError,
		Code:     "unknown-kind",
		Pos:      decl.Pos,
		Message:  fmt.Sprintf("extension %q does not serve kind %q", e.manifest.ID, decl.Kind),
	}}
}

// Bind captures the host; the process starts on the first Call.
func (e *External) Bind(decl extension.Declaration, host extension.HostContext) (extension.Instance, error) {
	e.mu.Lock()
	e.host = host
	e.mu.Unlock()
	return &externalInstance{ext: e, decl: decl}, nil
}

// Close shuts the child down gracefully, then kills it if it overstays.
func (e *External) Close() error {
	e.mu.Lock()
	p := e.proc
	e.proc = nil
	e.mu.Unlock()
	if p == nil {
		return nil
	}
	done := make(chan struct{})
	go func() { p.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
	}
	if !p.isClosed() {
		p.kill()
	}
	return nil
}

// process returns a live child, starting or restarting it as needed.
func (e *External) process(ctx context.Context) (*process, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.proc != nil && !e.proc.isClosed() {
		return e.proc, nil
	}
	if e.proc != nil {
		// The previous child died. Respawn, up to the cap.
		if e.restarts >= maxRestarts {
			return nil, fmt.Errorf("extension %q keeps exiting (restarted %d times): %w", e.manifest.ID, e.restarts, e.proc.exitOrGone())
		}
		e.restarts++
	}

	if _, rerr := e.manifest.HostExecutableRel(); rerr != nil {
		return nil, fmt.Errorf("starting extension %q: %w", e.manifest.ID, rerr)
	}
	p, err := startProcess(e.manifest.ExecutablePath(), e.manifest.Args, e.childEnv(), &hostBridge{
		host:     e.host,
		manifest: e.manifest,
	})
	if err != nil {
		return nil, fmt.Errorf("starting extension %q: %w", e.manifest.ID, err)
	}
	if _, err := handshake(ctx, p); err != nil {
		p.kill()
		return nil, fmt.Errorf("extension %q handshake: %w", e.manifest.ID, err)
	}
	e.proc = p
	return p, nil
}

// childEnv is the environment the extension process runs with. Bloc A keeps
// it minimal on purpose — no ambient secrets; the extension asks for
// credentials through secret.resolve, which the manifest gates.
func (e *External) childEnv() []string { return e.manifest.launchEnv() }

func handshake(ctx context.Context, p *process) (initializeResult, error) {
	raw, err := p.call(ctx, "initialize", initializeParams{
		APIVersion:  APIVersion,
		Host:        hostName,
		HostVersion: hostVersion,
	})
	if err != nil {
		return initializeResult{}, err
	}
	var res initializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return initializeResult{}, fmt.Errorf("malformed initialize result: %w", err)
	}
	if res.APIVersion != APIVersion {
		return res, fmt.Errorf("extension speaks API %q, host speaks %q", res.APIVersion, APIVersion)
	}
	return res, nil
}

// externalInstance is one bound declaration. Every instance of one External
// shares that External's single child process.
type externalInstance struct {
	ext  *External
	decl extension.Declaration
}

func (i *externalInstance) Methods() []extension.MethodSpec {
	for _, spec := range i.ext.manifest.Declares {
		if spec.Kind == i.decl.Kind {
			return spec.Methods
		}
	}
	return nil
}

func (i *externalInstance) Call(ctx context.Context, req extension.CallRequest) (extension.Value, error) {
	p, err := i.ext.process(ctx)
	if err != nil {
		return nil, err
	}

	raw, err := p.call(ctx, "call", callParams{
		Declaration: mustRaw(req.Declaration),
		Operation:   req.Method,
		Args:        rawSlice(req.Args),
		NamedArgs:   rawMap(req.NamedArgs),
	})
	if err != nil {
		// Redact anything the extension emitted that reaches this error —
		// its message and any captured stderr — so a credential it resolved
		// through secret.resolve cannot leak back out through a failure.
		redact := i.ext.redactor()
		if tail := p.stderrText(); tail != "" && p.isClosed() {
			return nil, fmt.Errorf("%s.%s: %s\n--- extension stderr ---\n%s",
				i.decl.Name, req.Method, redact(err.Error()), redact(tail))
		}
		return nil, fmt.Errorf("%s.%s: %s", i.decl.Name, req.Method, redact(err.Error()))
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("%s.%s: decoding result: %w", i.decl.Name, req.Method, err)
	}
	return v, nil
}

// hostBridge adapts extension.HostContext to the process's inboundHandler,
// enforcing the manifest's secret allow-list.
type hostBridge struct {
	host     extension.HostContext
	manifest *Manifest
}

func (b *hostBridge) resolveSecret(ref string) (string, error) {
	if !b.manifest.AllowsSecret(ref) {
		return "", fmt.Errorf("manifest does not permit resolving %s", ref)
	}
	if b.host == nil {
		return "", fmt.Errorf("no host secret resolver available")
	}
	return b.host.ResolveSecret(ref)
}

func (b *hostBridge) logLine(msg string) {
	if b.host != nil {
		b.host.Logf("%s", msg)
	}
}

// alive reports whether the child process is currently up. Used by Smoke to
// tell "the extension answered with an error" (still alive, protocol-ok) from
// "the extension crashed or the pipe broke" (dead, a real failure).
func (e *External) alive() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.proc != nil && !e.proc.isClosed()
}

// redactor returns a function that masks known secrets in a string, via the
// bound host. It falls back to identity when no host is set (tests, metadata
// paths).
func (e *External) redactor() func(string) string {
	e.mu.Lock()
	host := e.host
	e.mu.Unlock()
	if host == nil {
		return func(s string) string { return s }
	}
	return host.Redact
}

func rawSlice(vs []extension.Value) []json.RawMessage {
	if len(vs) == 0 {
		return nil
	}
	out := make([]json.RawMessage, len(vs))
	for i, v := range vs {
		out[i] = mustRaw(v)
	}
	return out
}

func rawMap(m map[string]extension.Value) map[string]json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		out[k] = mustRaw(v)
	}
	return out
}
