import { memo, useMemo } from "react";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import { Merge, Timer } from "lucide-react";
import { importedDeclarations, openFile, revealSpan, startRun } from "../app/controller";
import { useStore } from "../app/store";
import { CALL_ICON, CALL_ICON_FALLBACK, EXIT_BADGE, StateBadge, type BadgeKind } from "../lib/icons";
import type { CallSite, Declaration } from "../lib/types";
import { stripMhExt } from "../lib/mhfile";
import type { ImportOrigin } from "../sync/imports";
import { useImportOrigins } from "./ImportsCard";
import { ARM_ROW_H, MAX_BOXES, MERGE_H, STAGE_ROW_H, type BoundaryNodeData, type ExternalNodeData, type RouteNodeData, type StageNodeData, type StepNodeData, type TerminalNodeData } from "./layout";

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
function CallBox({ call, kind, origin, at }: { call: CallSite; kind?: Declaration["kind"]; origin?: ImportOrigin; at?: string }) {
  const args = call.text.slice(call.receiver.length + 1 + call.method.length);
  const file = origin ? stripMhExt(origin.from.split("/").pop()!) : undefined;
  const Icon = CALL_ICON[kind ?? ""] ?? CALL_ICON_FALLBACK;
  return (
    <div
      className={`call-box kind-${kind ?? "other"}`}
      title={`${kind ?? "call"}: ${call.text}\nClick to show it in the code`}
      onClick={(e) => {
        e.stopPropagation();
        void revealSpan(call.span, at);
      }}
    >
      <Icon className="call-icon" size={12} strokeWidth={2} aria-hidden="true" />
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

/** The file chip of a node another fragment declares (a merged partial workflow): read-only here, click opens it. */
export function FragmentChip({ file }: { file: string }) {
  const base = file.split(/[\\/]/).pop() ?? file;
  return (
    <button
      className="call-origin fragment-chip"
      title={`declared in ${file}\nEdit it there — click to open it`}
      onClick={(e) => {
        e.stopPropagation();
        void openFile(file);
      }}
    >
      ↗{stripMhExt(base)}
    </button>
  );
}

function kindOf(data: StepNodeData): { kind: BadgeKind; label: string } {
  const n = data.node;
  // A gate (`pause(..., decision: T)`) is the step's defining trait, even when it also branches on the answer.
  if (n.decisions?.length) return { kind: "approval", label: `Gate · ${n.decisions.join(", ")}` };
  if (n.choice) return { kind: "choice", label: `Choice · ${n.choice.subject}` };
  if (n.calls.length) return { kind: "task", label: "Task" };
  if (n.exits.includes("pause")) return { kind: "approval", label: "Human approval" };
  if (n.exits.includes("fail") && !n.fallsThrough && n.exits.length === 1) return { kind: "fail", label: "Fail" };
  if ((n.exits.includes("complete") || n.exits.includes("break")) && !n.fallsThrough) return { kind: "succeed", label: "Succeed" };
  return { kind: "pass", label: "Step" };
}

/** A step's `timeout <dur>` badge. */
function TimeoutBadge({ timeout }: { timeout: string }) {
  return (
    <span className="badge icon" title={`timeout ${timeout}`}>
      <Timer size={10} strokeWidth={2} aria-hidden="true" />
      {timeout}
    </span>
  );
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
      {n.timeout && <TimeoutBadge timeout={n.timeout} />}
      {n.exits.map((x) => {
        const b = EXIT_BADGE[x];
        // A guard's fail(...) has no edge: its conditions are listed here.
        const when = x === "fail" && data.failsWhen?.length ? `${b?.title ?? "can fail the run"} when:\n${data.failsWhen.join("\n")}` : b?.title;
        return (
          <span key={x} className={`badge icon ${b?.cls ?? ""}`} title={when}>
            {b ? <b.Icon size={10} strokeWidth={2.25} aria-label={x} /> : x}
          </span>
        );
      })}
    </span>
  );
  return (
    <div className={`node step ${sites.length ? "has-calls" : ""} ${selected ? "selected" : ""} ${status ? `run-${status}` : ""} ${dropping ? "drop-target" : ""} ${focusCls} ${n.file ? "foreign" : ""}`}>
      <Handle type="target" position={Position.Top} />
      <div className="node-title">
        <StateBadge kind={k.kind} size={18} />
        <span className="node-name">{n.name}</span>
        {n.entry && <span className="badge" title="entry step">entry</span>}
        {n.file && <FragmentChip file={n.file} />}
        {sites.length > 0 && badges}
      </div>
      {sites.length > 0 ? (
        <div className="call-boxes">
          {sites.slice(0, MAX_BOXES).map((c, i) => (
            <CallBox key={i} call={c} kind={kinds[c.receiver]} origin={n.file ? undefined : origins.get(c.receiver)} at={n.file} />
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
    <div className={`node parallel ${selected ? "selected" : ""} ${status ? `run-${status}` : ""} ${data.node.file ? "foreign" : ""}`}>
      <Handle type="target" position={Position.Top} />
      <div className="node-title">
        <StateBadge kind="parallel" size={18} />
        <span className="node-name">{data.node.name}</span>
        <span className="node-kind">Parallel</span>
        {data.node.file && <FragmentChip file={data.node.file} />}
        {data.node.timeout && <TimeoutBadge timeout={data.node.timeout} />}
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
            void revealSpan(merge.span, data.node.file);
          }}
        >
          <Merge className="merge-icon" size={13} strokeWidth={2} aria-hidden="true" />
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
        <StateBadge kind="choice" size={18} />
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

/**
 * A stage — a `partial` fragment's header `stage Name`: a titled box around
 * that fragment's steps — a named phase, as a folder groups jobs. Its status is its steps' during a run;
 * double-click shows it in the code. A `goto Name` edge lands on it.
 */
export const StageNode = memo(function StageNode({ data }: NodeProps<Node<StageNodeData>>) {
  const statuses = useStore((s) => s.run.statuses);
  // A stopped run (paused or failed) of this file can redo the stage: `--resume --from`.
  const canRedo = useStore((s) => {
    const r = s.run;
    return !r.running && !!r.session && r.path === s.activePath && !!r.result && (!!r.result.paused || !r.result.ok);
  });
  const ks = data.steps.map((k) => statuses[k]).filter(Boolean);
  const status = ks.includes("failed")
    ? "failed"
    : ks.includes("running")
      ? "running"
      : ks.includes("paused")
        ? "paused"
        : ks.length === data.steps.length && ks.every((k) => k === "succeeded")
          ? "succeeded"
          : undefined;
  const s = data.stage;
  const redo = canRedo && (
    <button
      className="call-origin stage-redo"
      title={`Redo stage ${s.name}: resume the stopped run at ${s.members[0]}, with its variables as of the stop (mhl run --resume --from ${s.name})`}
      onClick={(e) => {
        e.stopPropagation();
        void startRun([], { resume: true, from: s.name });
      }}
    >
      ↻ redo
    </button>
  );
  if (data.collapsed) {
    // One card for the whole stage: its steps listed, a click opens them (WorkflowCanvas).
    const more = s.members.length - MAX_BOXES;
    const shown = more > 0 ? s.members.slice(0, MAX_BOXES) : s.members;
    return (
      <div
        className={`node stage collapsed ${status ? `run-${status}` : ""} ${s.file ? "foreign" : ""}`}
        title={`stage ${s.name}: ${s.members.join(", ")}\ngoto ${s.name} enters it at ${s.members[0]}.\nClick to open its steps${s.file ? ` (in ${s.file.split(/[\\/]/).pop()})` : ""}.`}
      >
        <Handle type="target" position={Position.Top} />
        <div className="stage-title">
          <span className="node-name">{s.name}</span>
          {redo}
          <span className="stage-open" aria-hidden="true">
            ›
          </span>
        </div>
        <div className="stage-sub">
          <span className="node-kind">Stage · {s.members.length} {s.members.length === 1 ? "step" : "steps"}</span>
          {s.file && <FragmentChip file={s.file} />}
        </div>
        <div className="stage-members mono">
          {shown.map((m) => (
            <div key={m} className={`stage-member ${statuses[m] ? `run-${statuses[m]}` : ""}`} style={{ height: STAGE_ROW_H }}>
              {m}
            </div>
          ))}
          {more > 0 && (
            <div className="stage-member more" style={{ height: STAGE_ROW_H }}>
              +{more} more
            </div>
          )}
        </div>
        <Handle type="source" position={Position.Bottom} isConnectable={false} />
      </div>
    );
  }
  return (
    <div
      className={`node stage ${status ? `run-${status}` : ""} ${s.file ? "foreign" : ""}`}
      title={`stage ${s.name}: ${s.members.join(", ")}\ngoto ${s.name} enters it at ${s.members[0]}. Double-click to show it in the code.`}
      onDoubleClick={(e) => {
        e.stopPropagation();
        void revealSpan(s.span, s.file);
      }}
    >
      <Handle type="target" position={Position.Top} />
      <div className="stage-title">
        <span className="node-kind">Stage</span>
        <span className="node-name">{s.name}</span>
        {redo}
        {s.file && <FragmentChip file={s.file} />}
      </div>
    </div>
  );
});

/**
 * Outside the open stage: a step or route its steps come from or go to.
 * Click goes back to the whole workflow (with the step selected).
 */
export const BoundaryNode = memo(function BoundaryNode({ data }: NodeProps<Node<BoundaryNodeData>>) {
  const status = useStatus(data.kind === "step" ? data.id : "");
  return (
    <div className={`node external boundary ${status ? `run-${status}` : ""}`} title={`${data.kind} ${data.name}, outside this stage\nClick to go back to the whole workflow`}>
      <Handle type="target" position={Position.Top} />
      {data.kind === "route" ? "◇" : "↗"} {data.name}
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </div>
  );
});

export const nodeTypes = {
  step: StepNode,
  parallel: ParallelNode,
  terminal: TerminalNode,
  external: ExternalNode,
  route: RouteNode,
  stage: StageNode,
  boundary: BoundaryNode,
};
