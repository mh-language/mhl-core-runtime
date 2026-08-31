package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// sessionTTL bounds how long an idle HTTP session stays resolvable; an
// `initialize` sweeps anything older (mirrors a2aserver.taskTTL).
const sessionTTL = time.Hour

// mcpPath is the single Streamable-HTTP endpoint.
const mcpPath = "/mcp"

// HTTPConfig is the knob set for the Streamable-HTTP MCP server. Zero values
// are the defaults that reproduce today's behaviour, so a bare
// HTTPConfig{Dir: d} serves exactly as before. Phases 2-3 add fields here
// (token verifier, principal header, store extension) rather than growing the
// function signatures again.
type HTTPConfig struct {
	Addr     string // listen address, host:port (ServeHTTP only)
	Dir      string // directory of .mh workflows to expose
	Token    string // "" disables bearer auth
	StateDir string // "" uses a per-process temp dir removed on shutdown

	// PrincipalHeader, when set, is the header an authenticated upstream (an
	// API Gateway authorizer, Envoy) puts the caller's identity in; the
	// runtime keys run ownership on it. Requires Token — without the shared
	// gateway↔mhl bearer the header would be client-spoofable.
	PrincipalHeader string

	// Store, when non-nil, backs durable run/session state with an external
	// `store`-kind extension instead of the on-disk .mhl/state tree (Phase 3).
	// The run *registry* stays in-memory per pod (Phase 4 distributes it).
	Store KVStore

	// DrainTimeout, when > 0, is how long a shutdown waits for in-flight
	// async runs to finish before cancelling them. 0 keeps the historical
	// behaviour: cancel immediately, then a hard 5s connection-drain.
	DrainTimeout time.Duration
	// MaxConcurrentRuns caps simultaneously executing runs; 0 is unlimited.
	MaxConcurrentRuns int
}

// ServeHTTP loads every .mh file under cfg.Dir and serves the MCP endpoint on
// cfg.Addr over the Streamable HTTP transport (JSON responses only — no SSE)
// until ctx is done. logw receives startup and per-request diagnostics. The
// caller registers session extensions (interpreter.SetSessionExtensions)
// beforehand.
func ServeHTTP(ctx context.Context, cfg HTTPConfig, logw io.Writer) error {
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	handler, h, err := buildHTTP(ctx, cfg, logw)
	if err != nil {
		_ = ln.Close()
		return err
	}
	fmt.Fprintf(logw, "mhl serve mcp --http: %d tool(s) from %s on http://%s%s\n",
		len(h.srv.tools), cfg.Dir, ln.Addr(), mcpPath)

	// BaseContext threads the server context into every request context, so a
	// SIGINT/SIGTERM cancels an in-flight tools/call run (matching the stdio
	// transport) rather than only draining connections for the grace window.
	httpSrv := &http.Server{
		Handler:     handler,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		h.drain(logw)
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(sh)
		h.cleanupRuns()
	}()
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// drain runs once when the server context is done. It flips readiness to 503
// (so the Service stops routing here and new run/start / tools/call are
// refused), then — when drainTimeout > 0 — waits up to that long for the
// in-flight async runs to reach a terminal state before cancelling them.
// drainTimeout == 0 keeps the historical behaviour: cancel at once.
func (h *httpServer) drain(logw io.Writer) {
	h.draining.Store(true)
	if h.drainTimeout <= 0 {
		h.srv.logEvent(slog.LevelInfo, "draining", "timeout", "0s", "cancelImmediately", true)
		h.runsCancel()
		return
	}
	deadline := time.Now().Add(h.drainTimeout)
	h.srv.logEvent(slog.LevelInfo, "draining", "timeout", h.drainTimeout.String(), "working", h.workingRuns())
	for time.Now().Before(deadline) {
		if h.workingRuns() == 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if n := h.workingRuns(); n > 0 {
		h.srv.logEvent(slog.LevelWarn, "drain deadline reached — cancelling runs", "working", n)
	}
	h.runsCancel()
}

// workingRuns counts async runs still executing.
func (h *httpServer) workingRuns() int {
	n := 0
	for _, rn := range h.runs.List() {
		rn.mu.Lock()
		working := rn.state == "working"
		rn.mu.Unlock()
		if working {
			n++
		}
	}
	return n
}

// Handler builds the MCP HTTP handler for the workflows under cfg.Dir — for
// embedding in another server or for tests. ServeHTTP is this plus an
// http.Server and signal handling. Async run state goes to a per-process temp
// dir; cfg.StateDir is ignored here (use HandlerWithState for a persistent
// one).
func Handler(cfg HTTPConfig, logw io.Writer) (http.Handler, error) {
	cfg.StateDir = ""
	handler, _, err := buildHTTP(context.Background(), cfg, logw)
	return handler, err
}

// HandlerWithState is Handler with a persistent async-run state directory:
// `run/*` state is written under cfg.StateDir and left in place, so a later
// process pointed at the same directory can `run/resume` a run it never
// started.
func HandlerWithState(ctx context.Context, cfg HTTPConfig, logw io.Writer) (http.Handler, error) {
	handler, _, err := buildHTTP(ctx, cfg, logw)
	return handler, err
}

type httpServer struct {
	srv *server
	// verifier extracts a verified principal per request (see verifier.go);
	// it replaces the historical inline bearer check.
	verifier TokenVerifier

	// baseCtx is the process lifetime (the SIGINT/SIGTERM context). A
	// synchronous tools/call descends from it via http.Server.BaseContext, so
	// it dies at once on a signal. runsCtx is a child of baseCtx that only the
	// drain path cancels: async runs descend from it, so --drain-timeout can
	// let them finish past the signal. runsCancel is idempotent.
	baseCtx    context.Context
	runsCtx    context.Context
	runsCancel context.CancelFunc

	// drainTimeout, when > 0, is how long shutdown waits for working runs
	// before calling runsCancel. 0 cancels immediately (historical).
	drainTimeout time.Duration

	// sem bounds simultaneously executing runs (async run/* and synchronous
	// tools/call alike). nil means unlimited — every acquire is a no-op.
	sem chan struct{}

	// metrics receives run lifecycle events; the built-in *promMetrics also
	// backs GET /metrics. See metrics.go.
	metrics MetricsSink

	// store is the external KV store backing durable state (nil ⇒ disk). Held
	// so execRun can build a per-run runtime.StateStore over it.
	store KVStore

	// State seams — see store.go. Each has one implementation today (a
	// process-local map, or the on-disk .mhl/state tree); Phase 3 swaps in
	// extension-backed ones for a fleet of replicas.
	sessions SessionStore
	runs     RunRegistry
	cps      CheckpointStore

	// draining is set once shutdown begins: /readyz then reports 503 so
	// Kubernetes stops routing to this pod, and new run/start / tools/call
	// are refused. /healthz stays 200 (the process is still alive).
	draining atomic.Bool
}

func buildHTTP(ctx context.Context, cfg HTTPConfig, logw io.Writer) (http.Handler, *httpServer, error) {
	tools, err := execsvc.Load(cfg.Dir)
	if err != nil {
		return nil, nil, err
	}

	// Durable state: an external store extension when configured, else the
	// on-disk .mhl/state tree. The run registry stays in-memory either way.
	var (
		sessions SessionStore = newMemSessionStore()
		cps      CheckpointStore
	)
	if cfg.Store != nil {
		sessions = newExtSessionStore(cfg.Store)
		cps, err = newExtCheckpointStore(cfg.Store)
	} else {
		cps, err = newDiskCheckpointStore(cfg.StateDir)
	}
	if err != nil {
		return nil, nil, err
	}

	runsCtx, runsCancel := context.WithCancel(ctx)
	var sem chan struct{}
	if cfg.MaxConcurrentRuns > 0 {
		sem = make(chan struct{}, cfg.MaxConcurrentRuns)
	}
	h := &httpServer{
		// Concurrent tools/call runs write their log()/step output here; a
		// mutex keeps lines from interleaving mid-write. slog does its own
		// locking, so it takes the raw writer.
		srv: &server{
			tools: tools,
			logw:  &syncWriter{w: logw},
			log:   slog.New(slog.NewJSONHandler(logw, nil)).With("component", "mcpserver"),
		},
		verifier:     newVerifier(cfg),
		baseCtx:      ctx,
		runsCtx:      runsCtx,
		runsCancel:   runsCancel,
		drainTimeout: cfg.DrainTimeout,
		sem:          sem,
		metrics:      newPromMetrics(),
		store:        cfg.Store,
		sessions:     sessions,
		runs:         newMemRunRegistry(),
		cps:          cps,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(mcpPath, h.handleMCP)
	// Per-method paths: POST /mcp/run/resume, /mcp/tools/call, … run the exact
	// same dispatch as POST /mcp, but let an external policy engine (an Istio
	// AuthorizationPolicy, an API Gateway route, a WAF rule) match one method
	// without parsing the JSON-RPC body. The body's `method` stays
	// authoritative; a mismatch with the path is -32600.
	mux.HandleFunc(mcpPath+"/", h.handleScopedMCP)
	h.registerOps(mux)
	return mux, h, nil
}

// handleMCP serves POST /mcp (and DELETE for session teardown) — the canonical
// single endpoint.
func (h *httpServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	h.serveMCP(w, r, "")
}

// handleScopedMCP serves POST /mcp/<method>. It is POST-only (no DELETE) and
// forwards <method> to serveMCP for a path/body consistency check.
func (h *httpServer) handleScopedMCP(w http.ResponseWriter, r *http.Request) {
	pathMethod := strings.TrimPrefix(r.URL.Path, mcpPath+"/")
	if pathMethod == "" || strings.Contains(pathMethod, "/../") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "this endpoint accepts POST (one JSON-RPC message)", http.StatusMethodNotAllowed)
		return
	}
	h.serveMCP(w, r, pathMethod)
}

// serveMCP is the shared handler for /mcp and /mcp/<method>. pathMethod, when
// non-empty, is the method the request path names; the body's `method` must
// equal it.
func (h *httpServer) serveMCP(w http.ResponseWriter, r *http.Request, pathMethod string) {
	principal, err := h.verifier.Verify(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// DNS-rebinding guard: a browser sends Origin; a non-browser client
	// usually does not. Reject any cross-origin request that is not loopback.
	if o := r.Header.Get("Origin"); o != "" && !originAllowed(o) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodPost:
		// handled below
	case http.MethodDelete:
		h.deleteSession(w, r)
		return
	default:
		// No server-to-client stream is offered, so GET (and anything else)
		// is 405 — spec-permitted for a JSON-only Streamable HTTP server.
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "this endpoint accepts POST (one JSON-RPC message) and DELETE (end a session)", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLine))
	if err != nil {
		writeRPC(w, http.StatusBadRequest, errMsg(nil, -32700, "read body: "+err.Error()))
		return
	}
	var msg rpcMsg
	if err := json.Unmarshal(body, &msg); err != nil {
		writeRPC(w, http.StatusBadRequest, errMsg(nil, -32700, "parse error: "+err.Error()))
		return
	}
	if pathMethod != "" && msg.Method != pathMethod {
		writeRPC(w, http.StatusBadRequest, errMsg(msg.ID, -32600,
			fmt.Sprintf("path %s/%s does not match body method %q", mcpPath, pathMethod, msg.Method)))
		return
	}

	// MCP-Protocol-Version is optional here (lenient when absent), but a
	// value this server does not implement is a client bug worth surfacing.
	if pv := r.Header.Get("MCP-Protocol-Version"); pv != "" && !handshakeVersions[pv] && pv != statelessVersion {
		writeRPC(w, http.StatusBadRequest, errMsg(msg.ID, -32602, "unsupported MCP-Protocol-Version: "+pv))
		return
	}

	// A draining server finishes what it has but accepts no new work.
	if h.draining.Load() && (msg.Method == "run/start" || msg.Method == "tools/call") {
		writeRPC(w, http.StatusServiceUnavailable, errMsg(msg.ID, -32000, "server is draining — not accepting new runs"))
		return
	}

	// A synchronous tools/call holds a concurrency slot for the whole run
	// (async run/start parks itself as "queued" instead — see launch). Wait a
	// short while for a slot, then shed load.
	if msg.Method == "tools/call" {
		release, ok := h.acquireSlotWait(r.Context(), 5*time.Second)
		if !ok {
			writeRPC(w, http.StatusServiceUnavailable, errMsg(msg.ID, -32000, "server at capacity — retry, or use run/start"))
			return
		}
		defer release()
	}

	// Resolve the protocol session: an existing HTTP session (by header), a
	// fresh one for `initialize`, or an ephemeral stateless one.
	var sess *session
	switch sid := r.Header.Get("Mcp-Session-Id"); {
	case sid != "":
		var ok bool
		sess, ok = h.sessions.Get(sid)
		if !ok {
			// Unknown/expired session — the client re-runs `initialize`.
			w.WriteHeader(http.StatusNotFound)
			return
		}

	case msg.Method == "initialize":
		h.sessions.SweepIdle(sessionTTL)
		h.sweepRuns()
		sess = &session{principal: principal}
		reply := h.srv.dispatch(r.Context(), sess, msg)
		if reply != nil && reply.Error == nil {
			sess.id = runtime.NewSessionID()
			h.sessions.Put(sess)
			w.Header().Set("Mcp-Session-Id", sess.id)
		}
		h.finish(w, reply)
		return

	default:
		// No session and not initializing: the stateless path. dispatch's
		// requireProtocolContext enforces params._meta (or -32602).
		sess = &session{}
	}
	// The principal is verified on every request — a stored legacy session
	// does not cache it, so an expired credential stops working at once.
	sess.principal = principal

	// run/* is this server's async-execution extension: start a workflow,
	// poll its step, cancel it — gated by the same protocol context as
	// tools/*. It is HTTP-only (it needs the run registry), so it is routed
	// here rather than in the transport-shared dispatch.
	if strings.HasPrefix(msg.Method, "run/") {
		if e := h.srv.requireProtocolContext(sess, msg); e != nil {
			h.finish(w, e)
			return
		}
		h.finish(w, h.handleRun(sess, msg))
		return
	}

	reply := h.srv.dispatch(r.Context(), sess, msg)
	if msg.Method == "tools/call" {
		h.metrics.ObserveToolCall(toolCallOutcome(reply))
	}
	h.finish(w, reply)
}

// toolCallOutcome classifies a tools/call reply: a protocol error or a tool
// result carrying isError:true is "error", anything else "ok".
func toolCallOutcome(reply *rpcMsg) string {
	if reply == nil || reply.Error != nil {
		return "error"
	}
	var r struct {
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(reply.Result, &r) == nil && r.IsError {
		return "error"
	}
	return "ok"
}

// finish writes a dispatch result: 202 for a notification (nil), else 200 +
// the JSON-RPC envelope.
func (h *httpServer) finish(w http.ResponseWriter, reply *rpcMsg) {
	if reply == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeRPC(w, http.StatusOK, reply)
}

func (h *httpServer) deleteSession(w http.ResponseWriter, r *http.Request) {
	if !h.sessions.Delete(r.Header.Get("Mcp-Session-Id")) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeRPC serialises one JSON-RPC envelope as the HTTP response body.
func writeRPC(w http.ResponseWriter, status int, m *rpcMsg) {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		http.Error(w, "marshal response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

// originAllowed reports whether an Origin header value is a loopback origin.
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

// syncWriter serialises concurrent writes to an underlying writer.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
