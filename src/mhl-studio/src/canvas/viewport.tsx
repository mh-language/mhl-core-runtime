// Keeping the diagram in view: "fit" (zoom to show all of it), "center"
// (pan to it at the current zoom) and "keep centered", which re-centers
// after every relayout and whenever the canvas is resized (a panel opened or
// closed, the split moved). The choice is remembered across launches.
import { ControlButton } from "@xyflow/react";

const KEY = "mhl-studio.canvas.keepCentered";

export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** Fit options shared by the button, the shortcut and a newly opened file. */
export const FIT = { padding: 0.08, maxZoom: 1, minZoom: 0.4 } as const;

/**
 * The flow point to put at the middle of a `view`-sized canvas so `bounds`
 * is centered at `zoom`: horizontally always; vertically too when it fits,
 * else its top stays in view (`pad` screen pixels below the edge), since a
 * workflow reads top-down from Start.
 */
export function centerPoint(bounds: Rect, view: { width: number; height: number }, zoom: number, pad = 24): { x: number; y: number } {
  const x = bounds.x + bounds.width / 2;
  const visible = view.height / zoom;
  const tall = bounds.height + (2 * pad) / zoom > visible;
  const y = tall ? bounds.y - pad / zoom + visible / 2 : bounds.y + bounds.height / 2;
  return { x, y };
}

export function loadKeepCentered(): boolean {
  try {
    return localStorage.getItem(KEY) !== "false";
  } catch {
    return true;
  }
}

export function saveKeepCentered(on: boolean) {
  try {
    localStorage.setItem(KEY, String(on));
  } catch {
    /* storage unavailable: the choice lasts this session */
  }
}

const icon = { width: 16, height: 16, viewBox: "0 0 16 16", fill: "none", stroke: "currentColor", strokeWidth: 1.6, strokeLinecap: "round" as const };

export function ViewportButtons(props: { onFit: () => void; onCenter: () => void; keepCentered: boolean; onToggle: () => void }) {
  return (
    <>
      <ControlButton onClick={props.onFit} title="Fit the whole diagram (⇧1)" aria-label="Fit the whole diagram">
        <svg {...icon}>
          <path d="M1.5 5V1.5H5M11 1.5h3.5V5M14.5 11v3.5H11M5 14.5H1.5V11" fill="none" />
        </svg>
      </ControlButton>
      <ControlButton onClick={props.onCenter} title="Center the diagram, keeping the zoom (⇧2)" aria-label="Center the diagram">
        <svg {...icon}>
          <circle cx="8" cy="8" r="4.5" fill="none" />
          <path d="M8 1v2.5M8 12.5V15M1 8h2.5M12.5 8H15" fill="none" />
          <circle cx="8" cy="8" r="1.2" fill="currentColor" stroke="none" />
        </svg>
      </ControlButton>
      <ControlButton
        onClick={props.onToggle}
        className={props.keepCentered ? "active" : ""}
        aria-pressed={props.keepCentered}
        title={
          props.keepCentered
            ? "Keeping the diagram centered after each change and resize — click to stop"
            : "Keep the diagram centered after each change and resize"
        }
        aria-label="Keep the diagram centered"
      >
        <svg {...icon}>
          <path d="M8 1.5v4M6.2 3.8 8 5.6l1.8-1.8M8 14.5v-4M6.2 12.2 8 10.4l1.8 1.8M1.5 8h4M3.8 6.2 5.6 8l-1.8 1.8M14.5 8h-4M12.2 6.2 10.4 8l1.8 1.8" fill="none" />
        </svg>
      </ControlButton>
    </>
  );
}
