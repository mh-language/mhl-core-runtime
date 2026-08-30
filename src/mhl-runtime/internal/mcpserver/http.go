package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// sessionTTL bounds how long an idle HTTP session stays resolvable; an
// `initialize` sweeps anything older (mirrors a2aserver.taskTTL).
const sessionTTL = time.Hour

// mcpPath is the single Streamable-HTTP endpoint.
const mcpPath = "/mcp"

// ServeHTTP loads every .mh file under dir and serves the MCP endpoint on
// addr over the Streamable HTTP transport (JSON responses only — no SSE)
// until ctx is done. token, when non-empty, is the bearer token every
// request must carry. stateDir, when non-empty, is where async run state
// (`run/*`) is persisted so a run survives a restart and `run/resume` can
// continue it; "" uses a per-process temp dir removed on shutdown. logw
// receives startup and per-request diagnostics. The caller registers session
// extensions (interpreter.SetSessionExtensions) beforehand.
func ServeHTTP(ctx context.Context, addr, dir, token, stateDir string, logw io.Writer) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	handler, h, err := buildHTTP(ctx, dir, token, stateDir, logw)
	if err != nil {
		_ = ln.Close()
		return err
	}
	fmt.Fprintf(logw, "mhl serve mcp --http: %d tool(s) from %s on http://%s%s\n",
		len(h.srv.tools), dir, ln.Addr(), mcpPath)

	// BaseContext threads the server context into every request context, so a
	// SIGINT/SIGTERM cancels an in-flight tools/call run (matching the stdio
	// transport) rather than only draining connections for the grace window.
	httpSrv := &http.Server{
		Handler:     handler,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
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

// Handler builds the MCP HTTP handler for the workflows under dir — for
// embedding in another server or for tests. ServeHTTP is this plus an
// http.Server and signal handling. Async run state goes to a per-process
// temp dir (see HandlerWithState / ServeHTTP's stateDir for a persistent
// one).
func Handler(dir, token string, logw io.Writer) (http.Handler, error) {
	handler, _, err := buildHTTP(context.Background(), dir, token, "", logw)
	return handler, err
}

// HandlerWithState is Handler with a persistent async-run state directory:
// `run/*` state is written under stateDir and left in place, so a later
// process pointed at the same stateDir can `run/resume` a run it never
// started.
func HandlerWithState(ctx context.Context, dir, token, stateDir string, logw io.Writer) (http.Handler, error) {
	handler, _, err := buildHTTP(ctx, dir, token, stateDir, logw)
	return handler, err
}

type httpServer struct {
	srv   *server
	token string // "" disables auth

	// baseCtx is the process lifetime: an async run's context descends from
	// it, so a server shutdown cancels every in-flight background run.
	baseCtx context.Context

	// runsDir is the parent of each async run's .mhl/ state tree. ownsRunsDir
	// is set when we created a temp one (removed on shutdown); a caller-given
	// stateDir is left in place so runs survive a restart.
	runsDir     string
	ownsRunsDir bool

	mu       sync.Mutex
	sessions map[string]*httpSession
	runs     map[string]*asyncRun
}

type httpSession struct {
	sess     *session
	lastUsed time.Time
}

func buildHTTP(ctx context.Context, dir, token, stateDir string, logw io.Writer) (http.Handler, *httpServer, error) {
	tools, err := execsvc.Load(dir)
	if err != nil {
		return nil, nil, err
	}
	runsDir, owns := stateDir, false
	if runsDir == "" {
		runsDir, err = os.MkdirTemp("", "mhl-serve-mcp-")
		if err != nil {
			return nil, nil, err
		}
		owns = true
	} else if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return nil, nil, err
	}
	h := &httpServer{
		// Concurrent tools/call runs write their log()/step output here; a
		// mutex keeps lines from interleaving mid-write.
		srv:         &server{tools: tools, logw: &syncWriter{w: logw}},
		token:       token,
		baseCtx:     ctx,
		runsDir:     runsDir,
		ownsRunsDir: owns,
		sessions:    map[string]*httpSession{},
		runs:        map[string]*asyncRun{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc(mcpPath, h.handleMCP)
	return mux, h, nil
}

func (h *httpServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if h.token != "" && r.Header.Get("Authorization") != "Bearer "+h.token {
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

	// MCP-Protocol-Version is optional here (lenient when absent), but a
	// value this server does not implement is a client bug worth surfacing.
	if pv := r.Header.Get("MCP-Protocol-Version"); pv != "" && !handshakeVersions[pv] && pv != statelessVersion {
		writeRPC(w, http.StatusBadRequest, errMsg(msg.ID, -32602, "unsupported MCP-Protocol-Version: "+pv))
		return
	}

	// Resolve the protocol session: an existing HTTP session (by header), a
	// fresh one for `initialize`, or an ephemeral stateless one.
	var sess *session
	switch sid := r.Header.Get("Mcp-Session-Id"); {
	case sid != "":
		sess = h.lookup(sid)
		if sess == nil {
			// Unknown/expired session — the client re-runs `initialize`.
			w.WriteHeader(http.StatusNotFound)
			return
		}

	case msg.Method == "initialize":
		h.sweepSessions()
		h.sweepRuns()
		sess = &session{}
		reply := h.srv.dispatch(r.Context(), sess, msg)
		if reply != nil && reply.Error == nil {
			sess.id = runtime.NewSessionID()
			h.store(sess)
			w.Header().Set("Mcp-Session-Id", sess.id)
		}
		h.finish(w, reply)
		return

	default:
		// No session and not initializing: the stateless path. dispatch's
		// requireProtocolContext enforces params._meta (or -32602).
		sess = &session{}
	}

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

	h.finish(w, h.srv.dispatch(r.Context(), sess, msg))
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
	sid := r.Header.Get("Mcp-Session-Id")
	h.mu.Lock()
	_, ok := h.sessions[sid]
	delete(h.sessions, sid)
	h.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *httpServer) lookup(sid string) *session {
	h.mu.Lock()
	defer h.mu.Unlock()
	hs := h.sessions[sid]
	if hs == nil {
		return nil
	}
	hs.lastUsed = time.Now()
	return hs.sess
}

func (h *httpServer) store(s *session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[s.id] = &httpSession{sess: s, lastUsed: time.Now()}
}

func (h *httpServer) sweepSessions() {
	cut := time.Now().Add(-sessionTTL)
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, hs := range h.sessions {
		if hs.lastUsed.Before(cut) {
			delete(h.sessions, id)
		}
	}
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
