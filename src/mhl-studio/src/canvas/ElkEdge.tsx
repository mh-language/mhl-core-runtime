import { memo } from "react";
import { BaseEdge, EdgeLabelRenderer, getSmoothStepPath, type Edge, type EdgeProps } from "@xyflow/react";
import type { FlowEdgeData, Point } from "./layout";

/** An SVG path through ELK's orthogonal route, with rounded corners. */
function roundedPath(points: Point[], r = 8): string {
  if (points.length < 2) return "";
  let d = `M ${points[0].x} ${points[0].y}`;
  for (let i = 1; i < points.length - 1; i++) {
    const p = points[i - 1];
    const c = points[i];
    const n = points[i + 1];
    const d1 = Math.hypot(c.x - p.x, c.y - p.y);
    const d2 = Math.hypot(n.x - c.x, n.y - c.y);
    const rr = Math.min(r, d1 / 2, d2 / 2);
    const a = { x: c.x - ((c.x - p.x) / (d1 || 1)) * rr, y: c.y - ((c.y - p.y) / (d1 || 1)) * rr };
    const b = { x: c.x + ((n.x - c.x) / (d2 || 1)) * rr, y: c.y + ((n.y - c.y) / (d2 || 1)) * rr };
    d += ` L ${a.x} ${a.y} Q ${c.x} ${c.y} ${b.x} ${b.y}`;
  }
  const last = points[points.length - 1];
  return d + ` L ${last.x} ${last.y}`;
}

export type ElkEdgeType = Edge<FlowEdgeData & { taken?: boolean }>;

export const ElkEdge = memo(function ElkEdge(props: EdgeProps<ElkEdgeType>) {
  const { data, selected, markerEnd } = props;
  const e = data!.edge;
  let path: string;
  let lx: number;
  let ly: number;
  if (data!.points.length >= 2) {
    path = roundedPath(data!.points);
    const mid = data!.labelAt ?? data!.points[Math.floor(data!.points.length / 2)];
    lx = mid.x;
    ly = mid.y;
  } else {
    [path, lx, ly] = getSmoothStepPath(props);
  }
  const cls = `edge kind-${e.kind} ${selected ? "selected" : ""} ${data!.taken ? "taken" : ""}`;
  return (
    <>
      <BaseEdge path={path} markerEnd={markerEnd} className={cls} interactionWidth={14} />
      {e.label && (
        <EdgeLabelRenderer>
          <div
            className={`edge-label kind-${e.kind} ${data!.taken ? "taken" : ""}`}
            style={{ transform: `translate(-50%, -50%) translate(${lx}px, ${ly}px)` }}
            title={e.via ? `via route ${e.via}` : undefined}
          >
            {e.label}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
});

export const edgeTypes = { elk: ElkEdge };
