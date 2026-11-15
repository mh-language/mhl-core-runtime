import { memo, useMemo } from "react";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import { importedDeclarations, openFile, revealSpan } from "../app/controller";
import { useStore } from "../app/store";
import type { CallSite, Declaration } from "../lib/types";
import type { ImportOrigin } from "../sync/imports";
import { useImportOrigins } from "./ImportsCard";
import { ARM_ROW_H, MAX_BOXES, MERGE_H, type ExternalNodeData, type RouteNodeData, type StepNodeData, type TerminalNodeData } from "./layout";

const CALL_ICON: Record<string, string> = { tool: "🛠", agent: "⚙", router: "⇄", extension: "⧉", memory: "▤", prompt: "❝" };

/** Declaration kind by name — this file's and what it imports — to pick each call box's icon. */
function useDeclKinds(): Record<string, Declaration["kind"]> {
  const graph = useStore((s) => s.graph);
  const library = useStore((s) => s.library);
  return useMemo(() => {
    const out: Record<string, Declaration["kind"]> = {};
    for (const d of [...importedDeclarations(graph), ...(graph?.declarations ?? [])]) out[d.name] = d.kind;
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graph, library]);
}

/** One action box: the call a step makes, like a Step Functions task. Click shows it in the code. */
function CallBox({ call, kind, origin }: { call: CallSite; kind?: Declaration["kind"]; origin?: ImportOrigin }) {
  const args = call.text.slice(call.receiver.length + 1 + call.method.length);
  const file = origin?.from.split("/").pop()?.replace(/\.mh$/, "");
  return (
    <div
      className={`call-box kind-${kind ?? "other"}`}
      title={`${kind ?? "call"}: ${call.text}\nClick to show it in the code`}
      onClick={(e) => {
        e.stopPropagation();
        revealSpan(call.span);
      }}
    >
      <span className="call-icon">{CALL_ICON[kind ?? ""] ?? "ƒ"}</span>
      <span className="call-target">
        {call.receiver}.{call.method}
      </span>
      <span className="call-args">{args}</span>
      {origin && (
        <button
          className="call-origin"
          title={`${origin.name !== call.receiver ? `${origin.name} as ${call.receiver}, ` : ""}imported from ${origin.from}\nClick to open it`}
          onClick={(e) => {
            e.stopPropagation();
            void openFile(origin.path);
          }}
        >
          ↗{file}
        </button>
      )}
    </div>
  );
}

const EXIT_BADGE: Record<string, { icon: string; title: string; cls: string }> = {
  pause: { icon: "⏸", title: "pauses for approval", cls: "warn" },
  fail: { icon: "✕", title: "can fail the run", cls: "bad" },
  complete: { icon: "✓", title: "can complete the run", cls: "good" },
  break: { icon: "⏹", title: "can stop the run (break)", cls: "good" },
};

function kindOf(data: StepNodeData): { icon: string; label: string } {
  const n = data.node;
  if (n.choice) return { icon: "◇", label: `Choice · ${n.choice.subject}` };
  if (n.calls.length) return { icon: "⚙", label: "Task" };
  if (n.exits.includes("pause")) return { icon: "👤", label: "Human approval" };
  if (n.exits.includes("fail") && !n.fallsThrough && n.exits.length === 1) return { icon: "✕", label: "Fail" };
  if ((n.exits.includes("complete") || n.exits.includes("break")) && !n.fallsThrough) return { icon: "✓", label: "Succeed" };
  return { icon: "▭", label: "Step" };
}

function useStatus(id: string) {
  return useStore((s) => s.run.statuses[id]);
}

export const StepNode = memo(function StepNode({ id, data, selected }: NodeProps<Node<StepNodeData>>) {
  const status = useStatus(id);
  const dropping = useStore((s) => s.dropTarget === id);
  const dropKind = useStore((s) => (s.dropTarget === id ? s.dropKind : null));
  const kinds = useDeclKinds();
  const origins = useImportOrigins();
  const focus = useStore((s) => s.importFocus);
  const k = kindOf(data);
  const n = data.node;
  const sites = n.callSites ?? [];
  const focusCls = !focus ? "" : sites.some((c) => focus.includes(c.receiver)) ? "import-hit" : "import-dim";
  const badges = (
    <span className="node-badges">
      {n.timeout && <span className="badge" title={`timeout ${n.timeout}`}>⏱ {n.timeout}</span>}
      {n.exits.map((x) => (
        <span key={x} className={`badge ${EXIT_BADGE[x]?.cls ?? ""}`} title={EXIT_BADGE[x]?.title}>
          {EXIT_BADGE[x]?.icon ?? x}
        </span>
      ))}
    </span>
  );
  return (
    <div className={`node step ${sites.length ? "has-calls" : ""} ${selected ? "selected" : ""} ${status ? `run-${status}` : ""} ${dropping ? "drop-target" : ""} ${focusCls}`}>
      <Handle type="target" position={Position.Top} />
      <div className="node-title">
        <span className="node-icon">{k.icon}</span>
        <span className="node-name">{n.name}</span>
        {n.entry && <span className="badge" title="entry step">entry</span>}
        {sites.length > 0 && badges}
      </div>
      {sites.length > 0 ? (
        <div className="call-boxes">
          {sites.slice(0, MAX_BOXES).map((c, i) => (
            <CallBox key={i} call={c} kind={kinds[c.receiver]} origin={origins.get(c.receiver)} />
          ))}
          {sites.length > MAX_BOXES && <div className="call-more">+{sites.length - MAX_BOXES} more</div>}
        </div>
      ) : (
        <div className="node-sub">
          <span className="node-kind" title={k.label}>{k.label}</span>
          {badges}
        </div>
      )}
      {dropping && <div className="drop-hint">{dropKind === "prompt" ? "❝ use prompt" : dropKind === "schema" ? "{ } use schema" : "+ add call"}</div>}
      {status && <div className="node-status">{status}</div>}
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
});

export const ParallelNode = memo(function ParallelNode({ id, data, selected }: NodeProps<Node<StepNodeData>>) {
  const statuses = useStore((s) => s.run.statuses);
  const merge = data.node.merge;
  // The merge runs as one more step of the group, after every branch.
  const kids = [...(data.node.children ?? []), ...(merge ? [merge.stepName] : [])];
  const ks = kids.map((k) => statuses[k]).filter(Boolean);
  const status = ks.includes("failed")
    ? "failed"
    : ks.includes("running")
      ? "running"
      : ks.length === kids.length && ks.length > 0
        ? "succeeded"
        : statuses[id];
  const mergeStatus = merge ? statuses[merge.stepName] : undefined;
  return (
    <div className={`node parallel ${selected ? "selected" : ""} ${status ? `run-${status}` : ""}`}>
      <Handle type="target" position={Position.Top} />
      <div className="node-title">
        <span className="node-icon">⫴</span>
        <span className="node-name">{data.node.name}</span>
        <span className="node-kind">Parallel</span>
        {data.node.timeout && <span className="badge">⏱ {data.node.timeout}</span>}
        {!!data.node.vars?.length && (
          <span className="badge mono" title={`the group's own vars, alive only while it runs:\n${data.node.vars.map((v) => v.name).join(", ")}`}>
            var {data.node.vars.map((v) => v.name).join(", ")}
          </span>
        )}
      </div>
      {merge && (
        <div
          className={`merge-bar ${mergeStatus ? `run-${mergeStatus}` : ""}`}
          style={{ height: MERGE_H }}
          title={
            merge.param
              ? `merge (${merge.param}) { ... } — runs once after every branch and combines their vars (${merge.param}.<Step>.<var>)\nClick to show it in the code`
              : "merge { ... } — runs once after every branch\nClick to show it in the code"
          }
          onClick={(e) => {
            e.stopPropagation();
            revealSpan(merge.span);
          }}
        >
          <span className="merge-icon">⤓</span>
          <span className="mono">{merge.param ? `merge (${merge.param})` : "merge"}</span>
          {merge.calls.length > 0 && <span className="merge-calls mono">{merge.calls.join(", ")}</span>}
        </div>
      )}
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
});

export const TerminalNode = memo(function TerminalNode({ data }: NodeProps<Node<TerminalNodeData>>) {
  const label = data.role === "start" ? "Start" : data.role === "end" ? "End" : "Fail";
  return (
    <div className={`node terminal ${data.role}`}>
      {data.role !== "start" && <Handle type="target" position={Position.Top} />}
      {label}
      {data.role === "start" && <Handle type="source" position={Position.Bottom} isConnectable={false} />}
    </div>
  );
});

export const ExternalNode = memo(function ExternalNode({ data }: NodeProps<Node<ExternalNodeData>>) {
  return (
    <div className="node external" title="a step declared in another fragment of this partial workflow">
      <Handle type="target" position={Position.Top} />
      ↗ {data.name}
    </div>
  );
});

/**
 * A `route Name(param) { arms }`: a Choice state shared by every step that
 * runs `goto Name(x)`; its arms leave it as edges. One no step calls is
 * outside the flow: a card beside the diagram listing its arms, no edges.
 * Click shows it in the code.
 */
export const RouteNode = memo(function RouteNode({ data }: NodeProps<Node<RouteNodeData>>) {
  const { route: r, used } = data;
  // An `entry route` runs as a step of its own name.
  const status = useStatus(r.entry ? r.name : "");
  const arms = `${r.arms.length} ${r.arms.length === 1 ? "arm" : "arms"}`;
  const how = r.entry ? `\nentry: the run starts by dispatching on input ${r.params[0] ?? ""}` : used ? "" : `\nNo step runs goto ${r.name}(...): the run never reaches it`;
  return (
    <div
      className={`node route ${used ? "" : "unused"} ${status ? `run-${status}` : ""}`}
      title={`route ${r.name}(${r.params.join(", ")}) — ${arms}${how}\nClick to show it in the code`}
      onClick={() => revealSpan(r.span)}
    >
      {used && <Handle type="target" position={Position.Top} isConnectable={false} />}
      <div className="node-title">
        <span className="node-icon">◇</span>
        <span className="node-name">{r.name}</span>
        <span className="node-kind mono">({r.params.join(", ")})</span>
        {r.entry && <span className="badge" title="entry route: the run starts here">entry</span>}
        {!used && <span className="badge warn">unused</span>}
      </div>
      {used ? (
        <div className="node-sub">
          <span className="node-kind">Route · {arms}</span>
        </div>
      ) : (
        <div className="route-arms mono">
          {r.arms.map((a, i) => (
            <div key={i} className="route-arm" style={{ height: ARM_ROW_H }}>
              <span className="route-pattern">{a.wildcard ? "otherwise" : a.pattern}</span>
              <span className="route-arrow">→</span>
              <span className={a.fail ? "route-fail" : "route-target"}>{a.fail ? "Fail" : a.target}</span>
            </div>
          ))}
        </div>
      )}
      {used && <Handle type="source" position={Position.Bottom} isConnectable={false} />}
    </div>
  );
});

export const nodeTypes = { step: StepNode, parallel: ParallelNode, terminal: TerminalNode, external: ExternalNode, route: RouteNode };
