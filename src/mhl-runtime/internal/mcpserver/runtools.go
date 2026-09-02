package mcpserver

import "encoding/json"

// The mhl_run_* "control tools" expose this server's run/* async-execution
// family (runs.go) to a plain MCP client that can only issue `tools/call` —
// VS Code, Claude Desktop, and most other MCP hosts have no plumbing for a
// custom JSON-RPC method like `run/start`. They are ordinary `tools/list`
// entries; the HTTP transport recognises a `tools/call` whose name is one of
// these and bridges it to the matching run/* method (serveMCP -> handleRun),
// then re-frames the reply as a CallToolResult (asToolResult).
//
// Only advertised when the server actually routes run/* (server.asyncRuns —
// the HTTP transport). Over stdio the family, and these tools, are absent.
// The raw run/* methods stay available for a policy gateway that matches on
// method name; the control tools are an additive second way in.
const (
	toolRunStart  = "mhl_run_start"
	toolRunStatus = "mhl_run_status"
	toolRunResume = "mhl_run_resume"
	toolRunCancel = "mhl_run_cancel"
	toolRunList   = "mhl_run_list"
	toolRunLogs   = "mhl_run_logs"
)

// runToolMethod maps a control-tool name to its run/* method and reports
// whether name is a control tool at all.
func runToolMethod(name string) (string, bool) {
	switch name {
	case toolRunStart:
		return "run/start", true
	case toolRunStatus:
		return "run/status", true
	case toolRunResume:
		return "run/resume", true
	case toolRunCancel:
		return "run/cancel", true
	case toolRunList:
		return "run/list", true
	case toolRunLogs:
		return "run/logs", true
	default:
		return "", false
	}
}

// bridgeRunToolParams converts a `tools/call` for a control tool into the
// params its run/* handler expects. For every tool but mhl_run_start the
// arguments object already is the run/* params; mhl_run_start renames its
// {workflow} field to run/start's {name}. The original params._meta (the
// stateless protocol context) is carried across so requireProtocolContext
// still sees it after the method rewrite.
func bridgeRunToolParams(name string, arguments, origParams json.RawMessage) json.RawMessage {
	out := map[string]json.RawMessage{}
	if name == toolRunStart {
		var a struct {
			Workflow  json.RawMessage `json:"workflow"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if len(arguments) > 0 {
			_ = json.Unmarshal(arguments, &a)
		}
		if len(a.Workflow) > 0 {
			out["name"] = a.Workflow
		}
		if len(a.Arguments) > 0 {
			out["arguments"] = a.Arguments
		}
	} else if len(arguments) > 0 {
		_ = json.Unmarshal(arguments, &out)
	}
	if len(origParams) > 0 {
		var op struct {
			Meta json.RawMessage `json:"_meta"`
		}
		if json.Unmarshal(origParams, &op) == nil && len(op.Meta) > 0 {
			out["_meta"] = op.Meta
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return arguments
	}
	return b
}

// asToolResult re-frames a run/* reply as the CallToolResult a `tools/call`
// client expects: the run-status object becomes structuredContent plus a
// pretty-printed text block. A protocol error (invalid params, unknown runId)
// passes straight through as a JSON-RPC error — the spec keeps those out of
// band, distinct from a tool that ran and failed.
func asToolResult(s *server, sess *session, reply *rpcMsg) *rpcMsg {
	if reply == nil || reply.Error != nil {
		return reply
	}
	var structured map[string]any
	if err := json.Unmarshal(reply.Result, &structured); err != nil {
		return s.replyResult(sess, reply.ID, toolResult(string(reply.Result), nil, false))
	}
	text, _ := json.MarshalIndent(structured, "", "  ")
	res := toolResult(string(text), structured, false)
	// When the reply names a run (start / resume / status / cancel), attach
	// resource_link items so a client can open the run's live logs and its
	// result as MCP resources instead of re-polling the tool.
	if id, _ := structured["runId"].(string); id != "" {
		logsURI, resURI := runResourceURIs(id)
		res["content"] = append(res["content"].([]map[string]any),
			map[string]any{
				"type": "resource_link", "uri": logsURI,
				"name": "run logs", "mimeType": mimeText,
				"description": "Live step/log() output for run " + id + ".",
			},
			map[string]any{
				"type": "resource_link", "uri": resURI,
				"name": "run result", "mimeType": mimeJSON,
				"description": "Status and final vars for run " + id + ".",
			},
		)
	}
	return s.replyResult(sess, reply.ID, res)
}

// runControlTools returns the six synthetic tools/list entries. workflowNames
// constrains mhl_run_start's `workflow` field to the loaded workflow set.
func runControlTools(workflowNames []string) []map[string]any {
	obj := func(props map[string]any, required ...string) map[string]any {
		m := map[string]any{
			"type":                 "object",
			"properties":           props,
			"additionalProperties": false,
		}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}
	freeObj := func(desc string) map[string]any {
		return map[string]any{"type": "object", "description": desc}
	}
	runID := map[string]any{
		"type":        "string",
		"description": "A runId returned by " + toolRunStart + ".",
	}
	return []map[string]any{
		{
			"name": toolRunStart,
			"description": "Start an mhl workflow asynchronously: returns a runId at once instead of blocking " +
				"for the whole run. Poll " + toolRunStatus + " for progress and the final vars; stream output " +
				"with " + toolRunLogs + ". Use this for a workflow that can run longer than the client's request " +
				"timeout, or one with a human-approval gate.",
			"inputSchema": obj(map[string]any{
				"workflow": map[string]any{
					"type":        "string",
					"enum":        workflowNames,
					"description": "Name of the workflow to run (one of the other tools in this list).",
				},
				"arguments": freeObj("Inputs for the workflow, matching that workflow's own inputSchema."),
			}, "workflow"),
		},
		{
			"name": toolRunStatus,
			"description": "Report an async run's state (queued/working/completed/failed/canceled), current step, " +
				"the steps it has reached, and — once completed — its final vars.",
			"inputSchema": obj(map[string]any{"runId": runID}, "runId"),
		},
		{
			"name": toolRunResume,
			"description": "Resume a failed or canceled run from its per-step checkpoint. arguments are merged over " +
				"the original inputs — the place to pass an approval decision a human-in-the-loop gate step reads.",
			"inputSchema": obj(map[string]any{
				"runId":     runID,
				"arguments": freeObj("Inputs merged over the original run's arguments."),
			}, "runId"),
		},
		{
			"name": toolRunCancel,
			"description": "Request cancellation of an async run. It stops at the next step boundary; a step already " +
				"in flight still finishes.",
			"inputSchema": obj(map[string]any{"runId": runID}, "runId"),
		},
		{
			"name":        toolRunList,
			"description": "List the async runs this caller has started, with each run's current state.",
			"inputSchema": obj(map[string]any{}),
		},
		{
			"name": toolRunLogs,
			"description": "Return an async run's retained step/log() output from a byte cursor. Reply: " +
				"{text, nextSince, dropped?}. Poll with the previous nextSince to stream.",
			"inputSchema": obj(map[string]any{
				"runId": runID,
				"since": map[string]any{
					"type":        "integer",
					"description": "Byte offset to resume from; 0 or omitted starts at the beginning.",
				},
			}, "runId"),
		},
	}
}
