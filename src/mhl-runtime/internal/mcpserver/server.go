// Package mcpserver exposes a directory of mhl pipelines/workflows as a
// Model Context Protocol server: one MCP tool per declaration, `tools/call`
// runs it through internal/execsvc and returns its final variable state.
//
// Two transports sit on the same message dispatch (server.dispatch):
//
//   - stdio (Serve) — newline-delimited JSON-RPC 2.0 over a byte stream, the
//     form an MCP client uses when it spawns `mhl serve mcp <dir>` as a
//     subprocess. Messages are handled one at a time in stream order.
//   - Streamable HTTP (ServeHTTP, http.go) — one JSON-RPC message per
//     `POST /mcp`, the form a networked MCP client uses.
//
// It is a dual-era server (2026-07-28 "Backward Compatibility with
// Initialization-Based Versions"): each connection's behaviour is selected
// from how the client opens it.
//
//   - An `initialize` request selects legacy handshake semantics (revisions
//     2025-11-25 / 2025-06-18 / 2025-03-26): the negotiated result carries no
//     `resultType` and puts serverInfo at the top level, and later `tools/*`
//     calls need no `params._meta`. Over HTTP the client then carries the
//     server-issued `Mcp-Session-Id` header on every following request.
//   - Otherwise the connection is modern/stateless (2026-07-28): every
//     `tools/*` / `ping` request MUST restate the protocol context in
//     `params._meta` (`io.modelcontextprotocol/protocolVersion` +
//     `.../clientCapabilities`) — a missing field is -32602, a
//     protocolVersion this server does not implement is -32022
//     (UnsupportedProtocolVersionError). Every result then carries
//     `resultType: "complete"` and `_meta.io.modelcontextprotocol/serverInfo`,
//     and `server/discover` (always the DiscoverResult shape) replaces
//     `initialize` for discovery.
//
// This is the server counterpart to internal/features/mcp (the client);
// they share no code.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// ServerVersion is reported as serverInfo.version. internal/cli overwrites
// it with the build version at startup.
var ServerVersion = "0"

// defaultHandshakeVersion is what `initialize` negotiates to when the
// client's advertised protocolVersion is one this server does not recognise.
const defaultHandshakeVersion = "2025-06-18"

// statelessVersion is the (project-defined) revision the stateless mode and
// `server/discover` advertise — see internal/features/mcp.SpecVersion.
const statelessVersion = "2026-07-28"

// handshakeVersions are the initialize/initialized-lifecycle revisions this
// server will echo back unchanged when a client advertises one of them.
var handshakeVersions = map[string]bool{
	"2025-11-25": true,
	"2025-06-18": true,
	"2025-03-26": true,
}

// params._meta keys the 2026-07-28 "Per-request protocol fields" define, plus
// the per-response serverInfo key a modern result SHOULD carry.
const (
	metaProtocolVersion = "io.modelcontextprotocol/protocolVersion"
	metaClientCaps      = "io.modelcontextprotocol/clientCapabilities"
	metaServerInfo      = "io.modelcontextprotocol/serverInfo"
)

const maxLine = 8 << 20 // 8 MiB, matching the external-extension transport cap

// listCacheTTLms is the `ttlMs` hint on cacheable results (server/discover,
// tools/list). The loaded workflow set never changes for the life of the
// process — it is read once at startup — so a long freshness window is
// honest; a restart is the only way it changes.
const listCacheTTLms = 300000

// session is the per-connection protocol state. On stdio one session lives
// for the whole process; over HTTP a session is created by `initialize` and
// keyed by the `Mcp-Session-Id` header, or is ephemeral (one request) for a
// stateless `params._meta` call.
type session struct {
	// initialized is set once the client completes the legacy `initialize`
	// handshake; before then every tools/* call is served statelessly and
	// must carry the 2026-07-28 params._meta protocol fields.
	initialized bool
	// protocol is the revision negotiated by `initialize` (legacy mode only).
	protocol string
	// id is the Mcp-Session-Id value (HTTP session mode only; "" otherwise).
	id string
	// principal is the verified caller identity (HTTP only, from
	// TokenVerifier), refreshed on every request. "" when no verifier yields
	// one — run ownership then falls back to the per-session hash.
	principal string
}

// Serve loads every .mh file under dir, then reads JSON-RPC messages from in
// and writes responses to out until in reaches EOF or ctx is done. logw
// receives human-readable diagnostics (load warnings) — never out, which is
// the raw protocol stream. The caller is responsible for registering
// session extensions (interpreter.SetSessionExtensions) beforehand.
func Serve(ctx context.Context, dir string, in io.Reader, out io.Writer, logw io.Writer) error {
	tools, err := execsvc.Load(dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(logw, "mhl serve mcp: %d tool(s) from %s\n", len(tools), dir)

	s := &server{tools: tools, logw: logw}
	sess := &session{}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg rpcMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			writeLine(out, errMsg(nil, -32700, "parse error: "+err.Error()))
			continue
		}
		if reply := s.dispatch(ctx, sess, msg); reply != nil {
			writeLine(out, reply)
		}
	}
	return sc.Err()
}

type server struct {
	tools map[string]execsvc.Workflow
	// logw is the diagnostics sink (stderr on stdio) — a running tool's
	// log()/step output goes here, never to the protocol stream.
	logw io.Writer
	// log is structured lifecycle logging (JSON to the diagnostics writer).
	// Non-nil for the HTTP transport; nil on stdio, where callers must
	// nil-check (see slog helper `logAttrs`).
	log *slog.Logger
	// asyncRuns is set by the HTTP transport, which serves the run/* method
	// family (runs.go). When true, `initialize` / `server/discover` advertise
	// it under capabilities.experimental["mhl.run"] so a client can discover
	// that a long pipeline must use run/start instead of a blocking
	// tools/call. Always false on stdio, where run/* is not routed.
	asyncRuns bool
}

// asyncRunMethods is the run/* family the HTTP transport serves. Advertised in
// the capability so the set is discoverable, not just documented.
var asyncRunMethods = []string{
	"run/start", "run/status", "run/resume", "run/cancel", "run/list", "run/logs",
}

// asyncRunCapabilityVersion is bumped when the run/* request/response shape
// changes in a way a client must adapt to.
const asyncRunCapabilityVersion = "1"

// capabilities is the object both `initialize` and `server/discover` return.
// `tools` is always present; `experimental["mhl.run"]` only when this server
// actually routes run/* (HTTP transport).
func (s *server) capabilities() map[string]any {
	caps := map[string]any{
		"tools":     map[string]any{"listChanged": false},
		"resources": map[string]any{"subscribe": false, "listChanged": false},
	}
	if s.asyncRuns {
		caps["experimental"] = map[string]any{
			"mhl.run": map[string]any{
				"version": asyncRunCapabilityVersion,
				"methods": asyncRunMethods,
			},
		}
	}
	return caps
}

// logEvent emits one structured lifecycle line, tolerating a nil logger
// (stdio never sets one).
func (s *server) logEvent(level slog.Level, msg string, args ...any) {
	if s == nil || s.log == nil {
		return
	}
	s.log.Log(context.Background(), level, msg, args...)
}

// writeLine marshals m as one newline-terminated JSON object.
func writeLine(out io.Writer, m *rpcMsg) {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	_, _ = out.Write(append(b, '\n'))
}

// dispatch routes one request and returns its response, or nil for a
// notification (no id) that needs no reply. It performs no I/O — the
// transport (Serve / handleMCP) serialises the result.
func (s *server) dispatch(ctx context.Context, sess *session, msg rpcMsg) *rpcMsg {
	notification := len(msg.ID) == 0 || string(msg.ID) == "null"

	switch msg.Method {
	case "initialize":
		// Legacy semantics: the handshake result carries no `resultType` and
		// puts serverInfo at the top level, matching revisions 2025-*.
		res := s.initializeResult(msg.Params)
		sess.initialized = true
		sess.protocol, _ = res["protocolVersion"].(string)
		return resultMsg(msg.ID, res)
	case "notifications/initialized":
		return nil // no-op (legacy lifecycle)
	case "server/discover":
		// Modern-only method, always the DiscoverResult shape. A cacheable
		// result (CacheableResult) — ttlMs/cacheScope are required.
		r := s.discoverResult()
		r["ttlMs"] = listCacheTTLms
		r["cacheScope"] = "public"
		decorateModern(r)
		return resultMsg(msg.ID, r)
	case "notifications/cancelled":
		// no-op: a request is handled to completion before the next is read,
		// so a cancellation can never be observed mid-flight (2026-07-28
		// Cancellation: a server MAY ignore when "the request cannot be
		// cancelled").
		return nil
	case "ping":
		// `ping` was removed in 2026-07-28 — honour it only for a legacy
		// (2025-11-25 and earlier) connection.
		if sess.initialized {
			return resultMsg(msg.ID, map[string]any{})
		}
		return errMsg(msg.ID, -32601, "method not found: ping")
	case "tools/list":
		if e := s.requireProtocolContext(sess, msg); e != nil {
			return e
		}
		if cur := listCursor(msg.Params); cur != "" {
			return errMsg(msg.ID, -32602, "unknown cursor: this server returns the full tool list unpaginated")
		}
		payload := map[string]any{"tools": s.toolList()}
		if !sess.initialized {
			payload["ttlMs"] = listCacheTTLms
			payload["cacheScope"] = "public"
		}
		return s.replyResult(sess, msg.ID, payload)
	case "tools/call":
		if e := s.requireProtocolContext(sess, msg); e != nil {
			return e
		}
		return s.callTool(ctx, sess, msg.ID, msg.Params)
	case "resources/list":
		if e := s.requireProtocolContext(sess, msg); e != nil {
			return e
		}
		if cur := listCursor(msg.Params); cur != "" {
			return errMsg(msg.ID, -32602, "unknown cursor: this server returns the full resource list unpaginated")
		}
		// The HTTP transport appends per-run resources before replying (see
		// serveMCP); over stdio the workflow resources are the whole list.
		return s.replyResult(sess, msg.ID, map[string]any{"resources": workflowResourceList(s.tools)})
	case "resources/read":
		if e := s.requireProtocolContext(sess, msg); e != nil {
			return e
		}
		return s.readResource(sess, msg)
	default:
		if notification {
			return nil
		}
		return errMsg(msg.ID, -32601, "method not found: "+msg.Method)
	}
}

// listCursor reads params.cursor from a *list request.
func listCursor(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var p struct {
		Cursor string `json:"cursor"`
	}
	_ = json.Unmarshal(params, &p)
	return p.Cursor
}

// replyResult builds a successful result response, adding the modern-mode
// decorations (`resultType: "complete"` and `_meta` serverInfo) unless a
// legacy `initialize` handshake is in effect for this session.
func (s *server) replyResult(sess *session, id json.RawMessage, payload map[string]any) *rpcMsg {
	if !sess.initialized {
		decorateModern(payload)
	}
	return resultMsg(id, payload)
}

func decorateModern(payload map[string]any) {
	if _, ok := payload["resultType"]; !ok {
		payload["resultType"] = "complete"
	}
	meta, _ := payload["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		payload["_meta"] = meta
	}
	meta[metaServerInfo] = map[string]any{"name": "mhl", "version": ServerVersion}
}

// requireProtocolContext gates a tools/* or ping request. On a legacy
// handshake session (initialize seen) it returns nil. On a modern connection
// it enforces 2026-07-28's per-request protocol fields: a missing
// io.modelcontextprotocol/protocolVersion or /clientCapabilities is a
// malformed request (-32602); a protocolVersion this server does not
// implement is UnsupportedProtocolVersionError (-32022, listing what it
// supports). A non-nil return is the error response to send.
func (s *server) requireProtocolContext(sess *session, msg rpcMsg) *rpcMsg {
	if sess.initialized {
		return nil
	}
	pv, hasPV, hasCaps := statelessMeta(msg.Params)
	if !hasPV || !hasCaps {
		return errMsg(msg.ID, -32602,
			"missing required params._meta fields "+metaProtocolVersion+" and "+metaClientCaps+" (or send `initialize` for the legacy handshake)")
	}
	if pv != statelessVersion {
		return errData(msg.ID, -32022, "Unsupported protocol version", map[string]any{
			"supported": []string{statelessVersion},
			"requested": pv,
		})
	}
	return nil
}

// statelessMeta reads the 2026-07-28 per-request protocol fields out of
// params._meta.
func statelessMeta(params json.RawMessage) (protocolVersion string, hasVersion, hasCaps bool) {
	if len(params) == 0 {
		return "", false, false
	}
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if json.Unmarshal(params, &p) != nil {
		return "", false, false
	}
	if raw, ok := p.Meta[metaProtocolVersion]; ok {
		hasVersion = true
		_ = json.Unmarshal(raw, &protocolVersion)
	}
	_, hasCaps = p.Meta[metaClientCaps]
	return protocolVersion, hasVersion, hasCaps
}

func (s *server) initializeResult(params json.RawMessage) map[string]any {
	pv := defaultHandshakeVersion
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 && json.Unmarshal(params, &p) == nil && handshakeVersions[p.ProtocolVersion] {
		pv = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": pv,
		"capabilities":    s.capabilities(),
		"serverInfo":      map[string]any{"name": "mhl", "version": ServerVersion},
	}
}

// discoverResult is the DiscoverResult body — `resultType` and the `_meta`
// serverInfo are added by decorateModern.
func (s *server) discoverResult() map[string]any {
	return map[string]any{
		"supportedVersions": []string{statelessVersion},
		"capabilities":      s.capabilities(),
	}
}

func (s *server) toolList() []map[string]any {
	names := make([]string, 0, len(s.tools))
	for n := range s.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	list := make([]map[string]any, 0, len(names))
	for _, n := range names {
		w := s.tools[n]
		desc := w.Pipeline.Description
		if desc == "" {
			desc = fmt.Sprintf("Runs the mhl %s %q.", w.KindLabel(), n)
		}
		entry := map[string]any{
			"name":        n,
			"description": desc,
			"inputSchema": w.Pipeline.InputSchema(),
		}
		if s.asyncRuns {
			// A pipeline can run longer than a gateway's request timeout;
			// tell the client it can also be driven async — via the raw
			// run/* method, or the mhl_run_start control tool for a client
			// that speaks only tools/call (see the experimental.mhl.run
			// capability and runtools.go).
			entry["_meta"] = map[string]any{
				"mhl.run": map[string]any{"async": true, "via": "run/start", "tool": toolRunStart},
			}
		}
		list = append(list, entry)
	}
	if s.asyncRuns {
		taken := make(map[string]bool, len(names))
		for _, n := range names {
			taken[n] = true
		}
		for _, ct := range runControlTools(names) {
			if taken[ct["name"].(string)] {
				s.logEvent(slog.LevelWarn,
					"async control tool shadowed by a workflow of the same name — skipping",
					"tool", ct["name"])
				continue
			}
			list = append(list, ct)
		}
	}
	return list
}

func (s *server) callTool(ctx context.Context, sess *session, id json.RawMessage, params json.RawMessage) *rpcMsg {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return errMsg(id, -32602, "invalid params: "+err.Error())
		}
	}
	w, ok := s.tools[p.Name]
	if !ok {
		return errMsg(id, -32602, fmt.Sprintf("unknown tool %q", p.Name))
	}
	// Enforce the advertised inputSchema before spending a run dir on it: a
	// missing required argument or an undeclared one is -32602 here, not a
	// late "undefined variable" once a step references it.
	if err := w.Pipeline.ValidateInputs(p.Arguments); err != nil {
		return errMsg(id, -32602, err.Error())
	}

	base, err := os.MkdirTemp("", "mhl-mcp-run-")
	if err != nil {
		return errMsg(id, -32603, "run dir: "+err.Error())
	}
	defer os.RemoveAll(base)

	res, runErr := execsvc.Run(execsvc.Request{
		Context:   ctx,
		Program:   w.Program,
		File:      w.File,
		Workflow:  w.Name,
		Inputs:    p.Arguments,
		BaseDir:   base,
		Principal: sess.principal,
		// A running tool's log()/step output goes to the diagnostics sink
		// (stderr), never to the protocol stream. The deprecated Logging
		// feature's own migration guidance for stdio is "log to stderr".
		Out: s.logw,
	})
	if runErr != nil {
		return s.replyResult(sess, id, toolResult(runErr.Error(), nil, true))
	}
	vars := res.Vars
	if vars == nil {
		vars = map[string]any{}
	}
	text, _ := json.MarshalIndent(vars, "", "  ")
	return s.replyResult(sess, id, toolResult(string(text), vars, false))
}

// readResource serves a resources/read for a mhl://workflow/... URI. The HTTP
// transport handles mhl://run/... itself (it needs the run registry) before
// this is reached; any other URI is an unknown resource.
func (s *server) readResource(sess *session, msg rpcMsg) *rpcMsg {
	var p struct {
		URI string `json:"uri"`
	}
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return errMsg(msg.ID, -32602, "invalid params: "+err.Error())
		}
	}
	if p.URI == "" {
		return errMsg(msg.ID, -32602, "resources/read requires a uri")
	}
	contents, err, ok := readWorkflowResource(s.tools, p.URI)
	if !ok {
		return errMsg(msg.ID, -32602, "unknown resource: "+p.URI)
	}
	if err != nil {
		return errMsg(msg.ID, -32602, err.Error())
	}
	return s.replyResult(sess, msg.ID, map[string]any{"contents": contents})
}

// toolResult builds a CallToolResult body. structured, when non-nil, is
// echoed as `structuredContent` alongside the text block (the spec's
// backward-compat guidance: a structured result SHOULD also appear as text).
func toolResult(text string, structured any, isError bool) map[string]any {
	r := map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
	if structured != nil {
		r["structuredContent"] = structured
	}
	return r
}
