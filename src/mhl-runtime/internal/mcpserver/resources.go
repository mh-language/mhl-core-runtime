package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// This server exposes read-only MCP resources — the detail a compact
// tools/list entry deliberately omits — each addressed by a mhl:// URI:
//
//	mhl://workflow/<name>          a JSON manifest: ordered steps, typed
//	                              inputs, checkpoint config, parallel groups,
//	                              and the agents/tools/memory declared in the
//	                              same program
//	mhl://workflow/<name>/source   the declaration's .mh source text
//	mhl://run/<runId>/logs         an async run's retained step/log() output
//	mhl://run/<runId>/result       an async run's status + final vars as JSON
//
// The workflow resources are transport-shared (Serve stdio and ServeHTTP);
// the run resources need the run registry and are HTTP-only, spliced in by
// serveMCP the same way run/* is. The manifest is derived entirely from the
// loaded Workflow / runtime.Pipeline so it cannot drift from what runs.
const (
	resURIScheme    = "mhl://"
	resWorkflowHost = "workflow/"
	resRunHost      = "run/"

	mimeJSON   = "application/json"
	mimeSource = "text/x-mhl"
	mimeText   = "text/plain"
)

// workflowResourceURIs returns the (manifest, source) URIs for a workflow.
func workflowResourceURIs(name string) (manifest, source string) {
	base := resURIScheme + resWorkflowHost + name
	return base, base + "/source"
}

// runResourceURIs returns the (logs, result) URIs for an async run.
func runResourceURIs(id string) (logs, result string) {
	base := resURIScheme + resRunHost + id
	return base + "/logs", base + "/result"
}

// workflowResourceList renders every loaded workflow as a manifest + source
// resource entry, ordered by name.
func workflowResourceList(tools map[string]execsvc.Workflow) []map[string]any {
	names := make([]string, 0, len(tools))
	for n := range tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, 2*len(names))
	for _, n := range names {
		w := tools[n]
		mURI, sURI := workflowResourceURIs(n)
		desc := w.Pipeline.Description
		if desc == "" {
			desc = fmt.Sprintf("The mhl %s %q.", w.KindLabel(), n)
		}
		out = append(out,
			map[string]any{
				"uri":         mURI,
				"name":        n + " (manifest)",
				"description": desc + " Steps, inputs, checkpoint and declared dependencies as JSON.",
				"mimeType":    mimeJSON,
			},
			map[string]any{
				"uri":         sURI,
				"name":        n + " (source)",
				"description": "The .mh source of " + n + ".",
				"mimeType":    mimeSource,
			},
		)
	}
	return out
}

// readWorkflowResource serves a mhl://workflow/... URI. ok is false when uri
// is not a workflow resource at all (the caller may try another handler); a
// well-formed URI naming an unknown workflow or sub-resource returns err with
// ok true.
func readWorkflowResource(tools map[string]execsvc.Workflow, uri string) (contents []map[string]any, err error, ok bool) {
	rest, isWF := strings.CutPrefix(uri, resURIScheme+resWorkflowHost)
	if !isWF {
		return nil, nil, false
	}
	name, sub, _ := strings.Cut(rest, "/")
	w, found := tools[name]
	if !found {
		return nil, fmt.Errorf("unknown workflow %q", name), true
	}
	switch sub {
	case "":
		b, _ := json.MarshalIndent(workflowManifest(w), "", "  ")
		return []map[string]any{{"uri": uri, "mimeType": mimeJSON, "text": string(b)}}, nil, true
	case "source":
		src, e := os.ReadFile(w.File)
		if e != nil {
			return nil, fmt.Errorf("reading source of %q: %v", name, e), true
		}
		return []map[string]any{{"uri": uri, "mimeType": mimeSource, "text": string(src)}}, nil, true
	default:
		return nil, fmt.Errorf("unknown sub-resource %q of workflow %q", sub, name), true
	}
}

// workflowManifest is the deep view of a workflow — everything a tools/list
// entry omits — projected from the loaded Workflow.
func workflowManifest(w execsvc.Workflow) map[string]any {
	p := w.Pipeline
	m := map[string]any{
		"name":        w.Name,
		"kind":        w.KindLabel(),
		"source":      resURIScheme + resWorkflowHost + w.Name + "/source",
		"steps":       p.Steps,
		"inputSchema": p.InputSchema(),
		// Mirrors the tools/list _meta.mhl.run hint.
		"async": map[string]any{"via": "run/start", "tool": toolRunStart},
	}
	if p.Description != "" {
		m["description"] = p.Description
	}
	if f := filepath.Base(w.File); f != "" && f != "." {
		m["file"] = f
	}

	inputs := make([]map[string]any, 0, len(p.Inputs))
	for _, in := range p.Inputs {
		inputs = append(inputs, map[string]any{
			"name": in.Name, "type": in.Type.String(), "required": true,
		})
	}
	m["inputs"] = inputs

	var groups []map[string]any
	for _, st := range p.Stages {
		if st.Parallel {
			groups = append(groups, map[string]any{"name": st.Name, "steps": st.Steps})
		}
	}
	if len(groups) > 0 {
		m["parallelGroups"] = groups
	}
	if len(p.StepTimeouts) > 0 {
		to := make(map[string]any, len(p.StepTimeouts))
		for k, v := range p.StepTimeouts {
			to[k] = v.String()
		}
		m["stepTimeouts"] = to
	}

	if p.Checkpoint.Enabled {
		cp := map[string]any{"strategy": p.Checkpoint.Strategy}
		if p.Checkpoint.TTL > 0 {
			cp["ttl"] = p.Checkpoint.TTL.String()
		}
		if p.Checkpoint.Storage != "" {
			cp["storage"] = p.Checkpoint.Storage
		}
		m["checkpoint"] = cp
		m["resumable"] = true
	}
	if p.Loop {
		loop := map[string]any{"stopWhen": p.StopWhen != nil}
		if p.MaxIterations > 0 {
			loop["maxIterations"] = p.MaxIterations
		}
		m["loop"] = loop
	}
	if decl := programDeclarations(w.Program); len(decl) > 0 {
		m["declared"] = decl
	}
	return m
}

// isRunResourceURI reports whether a resources/read params object names a
// mhl://run/... URI (served by the HTTP transport, not the shared dispatch).
func isRunResourceURI(params json.RawMessage) bool {
	if len(params) == 0 {
		return false
	}
	var p struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(params, &p)
	return strings.HasPrefix(p.URI, resURIScheme+resRunHost)
}

// runResourceList renders the calling caller's async runs as a logs + result
// resource pair each. Owner-scoped exactly like run/list.
func (h *httpServer) runResourceList(sess *session) []map[string]any {
	owner := h.ownerOf(sess)
	runs := h.runs.List()
	sort.Slice(runs, func(i, j int) bool { return runs[i].id < runs[j].id })
	out := make([]map[string]any, 0, 2*len(runs))
	for _, rn := range runs {
		if rn.owner != owner {
			continue
		}
		logsURI, resURI := runResourceURIs(rn.id)
		rn.mu.Lock()
		state, tool := rn.state, rn.tool.Name
		rn.mu.Unlock()
		out = append(out,
			map[string]any{
				"uri":         logsURI,
				"name":        tool + " run " + rn.id + " (logs)",
				"description": "Retained step/log() output of this run.",
				"mimeType":    mimeText,
			},
			map[string]any{
				"uri":         resURI,
				"name":        tool + " run " + rn.id + " (result)",
				"description": "Status and final vars of this run (state: " + state + ").",
				"mimeType":    mimeJSON,
			},
		)
	}
	return out
}

// readRunResource serves mhl://run/<id>/{logs,result}. Owner-gated like
// run/status: a non-owner, or an unknown id, gets -32602 "unknown resource"
// (so it is not an existence oracle).
func (h *httpServer) readRunResource(sess *session, msg rpcMsg) *rpcMsg {
	var p struct {
		URI string `json:"uri"`
	}
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return errMsg(msg.ID, -32602, "invalid params: "+err.Error())
		}
	}
	rest := strings.TrimPrefix(p.URI, resURIScheme+resRunHost)
	id, sub, _ := strings.Cut(rest, "/")
	rn := h.ownedRun(id, sess)
	if rn == nil {
		return errMsg(msg.ID, -32602, "unknown resource: "+p.URI)
	}
	h.refreshRemote(rn) // a reconstructed run advances on the owning replica
	switch sub {
	case "logs":
		text, _, _ := rn.logs.read(0)
		return h.srv.replyResult(sess, msg.ID, map[string]any{"contents": []map[string]any{
			{"uri": p.URI, "mimeType": mimeText, "text": text},
		}})
	case "result":
		b, _ := json.MarshalIndent(h.runView(rn), "", "  ")
		return h.srv.replyResult(sess, msg.ID, map[string]any{"contents": []map[string]any{
			{"uri": p.URI, "mimeType": mimeJSON, "text": string(b)},
		}})
	default:
		return errMsg(msg.ID, -32602, "unknown run sub-resource: "+p.URI)
	}
}

// programDeclarations lists the top-level agent / tool / memory / prompt /
// extension names declared in prog — the dependencies available to any
// workflow parsed from it. Name lists only.
func programDeclarations(prog *ast.Program) map[string]any {
	if prog == nil {
		return nil
	}
	var agents, tools, mems, prompts, exts []string
	for _, d := range prog.Decls {
		switch {
		case d.Agent != nil && d.Agent.Name != "":
			agents = append(agents, d.Agent.Name)
		case d.Tool != nil:
			tools = append(tools, d.Tool.Name)
		case d.Memory != nil:
			mems = append(mems, d.Memory.Name)
		case d.Prompt != nil:
			prompts = append(prompts, d.Prompt.Name)
		case d.Extension != nil:
			exts = append(exts, d.Extension.Name)
		}
	}
	out := map[string]any{}
	add := func(k string, v []string) {
		if len(v) > 0 {
			sort.Strings(v)
			out[k] = v
		}
	}
	add("agents", agents)
	add("tools", tools)
	add("memory", mems)
	add("prompts", prompts)
	add("extensions", exts)
	if len(out) == 0 {
		return nil
	}
	return out
}
