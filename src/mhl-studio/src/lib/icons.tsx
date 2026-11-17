// The studio's one icon vocabulary (Lucide). Each flow state has a fixed
// icon, shared by the states bar and the diagram's nodes, so the palette and
// the canvas read the same way. Icons are monochrome, like the toolbar's;
// only exit badges carry a semantic color.
import {
  ArrowLeftRight,
  Bot,
  CircleCheck,
  CircleStop,
  CircleX,
  Cog,
  Columns3,
  Database,
  Puzzle,
  Quote,
  Split,
  SquareDashed,
  SquareFunction,
  UserCheck,
  Wrench,
  type LucideIcon,
  type LucideProps,
} from "lucide-react";
import type { StateKind } from "../sync/templates";

/** Defaults for a toolbar-sized icon; spread first, then override. */
export const ICON: LucideProps = { size: 16, strokeWidth: 1.75, "aria-hidden": true };

/** What a state or a node is, for its icon: a palette state, or "task" for a step that makes calls. */
export type BadgeKind = StateKind | "task";

export const STATE_ICON: Record<BadgeKind, LucideIcon> = {
  pass: SquareDashed,
  agent: Bot,
  tool: Wrench,
  choice: Split,
  parallel: Columns3,
  approval: UserCheck,
  succeed: CircleCheck,
  fail: CircleX,
  task: Cog,
};

/** A state's icon on its tinted square — the one mark the states bar and the nodes share. */
export function StateBadge({ kind, size = 20 }: { kind: BadgeKind; size?: number }) {
  const Icon = STATE_ICON[kind];
  return (
    <span className="state-badge" style={{ width: size, height: size }} aria-hidden="true">
      <Icon size={Math.round(size * 0.65)} strokeWidth={2} />
    </span>
  );
}

/** A call box's icon, by the kind of declaration it calls. */
export const CALL_ICON: Record<string, LucideIcon> = {
  tool: Wrench,
  agent: Bot,
  router: ArrowLeftRight,
  extension: Puzzle,
  memory: Database,
  prompt: Quote,
};
export const CALL_ICON_FALLBACK = SquareFunction;

/** A step's exit badge (how it can leave the flow), in the matching state's icon. */
export const EXIT_BADGE: Record<string, { Icon: LucideIcon; title: string; cls: string }> = {
  pause: { Icon: UserCheck, title: "pauses for approval", cls: "warn" },
  fail: { Icon: CircleX, title: "can fail the run", cls: "bad" },
  complete: { Icon: CircleCheck, title: "can complete the run", cls: "good" },
  break: { Icon: CircleStop, title: "can stop the run (break)", cls: "good" },
};
