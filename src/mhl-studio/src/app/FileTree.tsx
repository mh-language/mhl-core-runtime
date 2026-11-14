// The side bar's file tree: the workspace's `.mh` and `.md` files under their
// folders.
//
// - Folders fold (remembered per workspace); the open file's folders always
//   unfold.
// - + (header, or a folder's) creates a file or folder; right-click any row
//   for New / Rename / Delete; F2 renames and Delete (⌘⌫) deletes the
//   selected row.
// - Drag a row onto a folder (or the tree's empty space, for the root) to move
//   it; references to moved files are rewritten (controller.movePath).
// - A Markdown file also drags onto the diagram as a prompt (app/prompts.ts),
//   a JSON file as an agent call's schema (app/schemas.ts).
import { useEffect, useMemo, useRef, useState, type ReactElement } from "react";
import { deletePath, movePath, newAgentFile, newFile, newFolder, openFile, refreshFiles } from "./controller";
import { AgentPicker } from "../palette/AgentPicker";
import { BACKENDS, ROLES, type Backend, type Role } from "../sync/agentTemplates";
import { isJson, isMarkdown, useStore } from "./store";
import { DRAG_MIME, PROMPT_MIME, SCHEMA_MIME } from "../canvas/WorkflowCanvas";
import { ancestors, buildTree, type TreeNode } from "./tree";
import { joinPath } from "../lib/api";
import { NEW_FILE_ITEMS, type NewFileItem } from "../sync/templates";
import fileIcon from "../assets/mhl-file.png";

/** What the + menu creates: a file of some kind, a folder, or a ready-made agent. */
type AgentChoice = { template: { backend: Backend; role: Role } };
type NewEntry = NewFileItem | "folder" | AgentChoice;
const isAgentChoice = (e: NewEntry): e is AgentChoice => typeof e === "object" && "template" in e;

/** A tree row moved by drag-and-drop. */
const PATH_MIME = "application/x-mhl-path";

type At = { top: number; left: number };

/** A row's identity: its relative path and whether it is a folder. */
interface Entry {
  rel: string;
  dir: boolean;
}

const parentOf = (rel: string) => (rel.includes("/") ? rel.slice(0, rel.lastIndexOf("/")) : "");
const baseOf = (rel: string) => rel.slice(rel.lastIndexOf("/") + 1);

function useEscape(onClose: () => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
}

function NewFileMenu({ at, title, onPick, onClose }: { at: At; title: string; onPick: (item: NewFileItem | "folder") => void; onClose: () => void }) {
  useEscape(onClose);
  return (
    <>
      <div className="menu-backdrop" onClick={onClose} onContextMenu={(e) => (e.preventDefault(), onClose())} />
      <div className="menu" role="menu" style={at}>
        <div className="menu-title">{title}</div>
        {NEW_FILE_ITEMS.map((item) => (
          <button key={item.kind} role="menuitem" className="menu-item" onClick={() => onPick(item)}>
            <span className="menu-icon">{item.icon}</span>
            <span>
              <span className="menu-label">{item.label}</span>
              <span className="menu-hint">{item.hint}</span>
            </span>
          </button>
        ))}
        <div className="menu-sep" />
        <button role="menuitem" className="menu-item" onClick={() => onPick("folder")}>
          <span className="menu-icon">
            <FolderIcon open={false} />
          </span>
          <span>
            <span className="menu-label">Folder</span>
            <span className="menu-hint">to group prompts, tools, flows…</span>
          </span>
        </button>
      </div>
    </>
  );
}

/** The right-click menu of a row (or of the tree's empty space: `entry` null). */
function RowMenu({
  at,
  entry,
  onNew,
  onRename,
  onDelete,
  onClose,
}: {
  at: At;
  entry: Entry | null;
  onNew: () => void;
  onRename: () => void;
  onDelete: () => void;
  onClose: () => void;
}) {
  useEscape(onClose);
  const item = (label: string, keys: string, run: () => void, danger = false) => (
    <button role="menuitem" className={`menu-item compact ${danger ? "danger" : ""}`} onClick={() => (onClose(), run())}>
      <span className="menu-label">{label}</span>
      <span className="spacer" />
      <span className="menu-keys">{keys}</span>
    </button>
  );
  return (
    <>
      <div className="menu-backdrop" onClick={onClose} onContextMenu={(e) => (e.preventDefault(), onClose())} />
      <div className="menu narrow" role="menu" style={at}>
        {(!entry || entry.dir) && item("New file or folder…", "", onNew)}
        {entry && (
          <>
            {entry.dir && <div className="menu-sep" />}
            {item("Rename…", "F2", onRename)}
            {item("Move to Trash", "⌘⌫", onDelete, true)}
          </>
        )}
      </div>
    </>
  );
}

/** Markdown's mark: a document with "M↓". */
function MarkdownIcon() {
  return (
    <svg className="tree-icon md" width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
      <rect x="0.75" y="3" width="14.5" height="10" rx="1.5" fill="none" stroke="currentColor" strokeWidth="1.2" />
      <path d="M3 10.5v-5l2 2.4 2-2.4v5M10.5 5.5v4.6M8.8 8.6l1.7 1.9 1.7-1.9" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round" />
    </svg>
  );
}

/** JSON's mark: a pair of braces. */
function JsonIcon() {
  return (
    <svg className="tree-icon json" width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
      <path d="M5.5 2.5C3.8 2.5 4 4 4 5.5S3.2 8 2 8c1.2 0 2 .9 2 2.5s-.2 3 1.5 3M10.5 2.5c1.7 0 1.5 1.5 1.5 3s.8 2.5 2 2.5c-1.2 0-2 .9-2 2.5s.2 3-1.5 3" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
    </svg>
  );
}

function FolderIcon({ open }: { open: boolean }) {
  return (
    <svg className="tree-icon folder" width="14" height="12" viewBox="0 0 14 12" aria-hidden="true">
      {open ? (
        <path d="M1 2.5A1 1 0 0 1 2 1.5h3l1.2 1.3H12a1 1 0 0 1 1 1V5H3.2L1.6 10.2 1 10V2.5Zm2.3 3.2H13.6l-1.6 5H1.8l1.5-5Z" fill="currentColor" />
      ) : (
        <path d="M1 2.5A1 1 0 0 1 2 1.5h3l1.2 1.3H12a1 1 0 0 1 1 1v6.2a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1V2.5Z" fill="currentColor" />
      )}
    </svg>
  );
}

/** Inline rename: the name selected up to its extension; Enter or blur commits, Esc cancels. */
function RenameInput({ rel, onDone }: { rel: string; onDone: (next: string | null) => void }) {
  const [value, setValue] = useState(baseOf(rel));
  const done = useRef(false);
  const finish = (next: string | null) => {
    if (done.current) return;
    done.current = true;
    onDone(next);
  };
  return (
    <input
      className="tree-rename"
      autoFocus
      value={value}
      spellCheck={false}
      onFocus={(e) => {
        const v = e.target.value;
        const ext = v.search(/(\.prompt\.md|\.schema\.json|\.mh|\.md|\.json)$/);
        e.target.setSelectionRange(0, ext <= 0 ? v.length : ext);
      }}
      onChange={(e) => setValue(e.target.value)}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === "Enter") finish(value.trim());
        if (e.key === "Escape") finish(null);
      }}
      onBlur={() => finish(value.trim())}
      onClick={(e) => e.stopPropagation()}
    />
  );
}

const storeKey = (root: string) => `mhl-studio.tree:${root}`;

function loadFolded(root: string): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(storeKey(root)) ?? "[]") as string[]);
  } catch {
    return new Set();
  }
}

/** The menu anchor for a button: under it, kept inside the window. */
function anchorOf(el: HTMLElement): At {
  const r = el.getBoundingClientRect();
  return { top: r.bottom + 4, left: Math.max(8, Math.min(r.left - 8, window.innerWidth - 298)) };
}

/** A menu anchor at the pointer, kept inside the window. */
function pointerAt(e: { clientX: number; clientY: number }): At {
  return { top: Math.min(e.clientY, window.innerHeight - 120), left: Math.min(e.clientX, window.innerWidth - 230) };
}

/** Whether `entry` may be dropped into folder `dir` ("" = the root). */
function canDrop(entry: Entry | null, dir: string): boolean {
  if (!entry) return false;
  if (parentOf(entry.rel) === dir) return false; // already there
  return !(entry.dir && (dir === entry.rel || dir.startsWith(`${entry.rel}/`))); // not into itself
}

// The row being dragged: dragover cannot read the drag data, only its types.
let dragging: Entry | null = null;

export function FileTree() {
  const root = useStore((s) => s.root);
  const files = useStore((s) => s.files);
  const dirs = useStore((s) => s.dirs);
  const activePath = useStore((s) => s.activePath);
  const dirty = useStore((s) => s.dirty);
  const [menu, setMenu] = useState<{ at: At; dir: string } | null>(null);
  const [rowMenu, setRowMenu] = useState<{ at: At; entry: Entry | null } | null>(null);
  const [creating, setCreating] = useState<{ item: NewEntry; dir: string } | null>(null);
  const [agentPick, setAgentPick] = useState<{ at: At; dir: string } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [selected, setSelected] = useState<Entry | null>(null);
  const [dropDir, setDropDir] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [folded, setFolded] = useState<Set<string>>(() => (root ? loadFolded(root) : new Set()));
  const unfoldTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const tree = useMemo(() => buildTree(files, dirs), [files, dirs]);
  const activeRel = useMemo(() => (root && activePath ? files.find((f) => joinPath(root, f) === activePath) ?? null : null), [root, activePath, files]);

  useEffect(() => {
    if (root) setFolded(loadFolded(root));
  }, [root]);

  // The open file's folders always unfold, so it is visible in the tree.
  useEffect(() => {
    if (!activeRel) return;
    setSelected({ rel: activeRel, dir: false });
    setFolded((f) => {
      const up = ancestors(activeRel).filter((d) => f.has(d));
      if (!up.length) return f;
      const next = new Set(f);
      up.forEach((d) => next.delete(d));
      return next;
    });
  }, [activeRel]);

  useEffect(() => {
    if (!root) return;
    try {
      localStorage.setItem(storeKey(root), JSON.stringify([...folded]));
    } catch {
      /* storage unavailable */
    }
  }, [root, folded]);

  if (!root) return null;

  const toggle = (dir: string) =>
    setFolded((f) => {
      const next = new Set(f);
      if (!next.delete(dir)) next.add(dir);
      return next;
    });
  const unfold = (dir: string) => setFolded((f) => (f.has(dir) ? new Set([...f].filter((d) => d !== dir)) : f));
  const allDirs = (nodes: TreeNode[]): string[] => nodes.flatMap((n) => (n.kind === "dir" ? [n.path, ...allDirs(n.children)] : []));

  const startNew = (item: NewEntry, dir: string) => {
    // An agent is picked from the ready-made templates first.
    if (item !== "folder" && !isAgentChoice(item) && item.kind === "agent") {
      setAgentPick({ at: menu?.at ?? { top: 80, left: 20 }, dir });
      setMenu(null);
      return;
    }
    setMenu(null);
    setCreating({ item, dir });
    const taken = new Set([...files, ...dirs]);
    const leaf = item === "folder" ? "prompts" : isAgentChoice(item) ? `${dir ? "" : "agents/"}${item.template.backend}_${item.template.role}.mh` : item.path;
    const base = (dir ? `${dir}/` : "") + leaf;
    let suggestion = base;
    for (let i = 2; taken.has(suggestion); i++) suggestion = base.replace(/(\.prompt\.md|\.schema\.json|\.mh|)$/, `_${i}$1`);
    setName(suggestion);
    if (dir) unfold(dir);
  };

  const rename = (entry: Entry, next: string | null) => {
    setRenaming(null);
    if (!next || next === baseOf(entry.rel)) return;
    // Relative to the parent folder; a name with "/" moves it deeper.
    const parent = parentOf(entry.rel);
    const to = parent ? `${parent}/${next}` : next;
    void movePath(entry.rel, to).then((ok) => ok && setSelected({ rel: to, dir: entry.dir }));
  };

  const remove = (entry: Entry) => void deletePath(entry.rel, entry.dir).then((ok) => ok && setSelected(null));

  const moveInto = (dir: string) => {
    const entry = dragging;
    dragging = null;
    setDropDir(null);
    if (!entry || !canDrop(entry, dir)) return;
    const to = (dir ? `${dir}/` : "") + baseOf(entry.rel);
    void movePath(entry.rel, to).then((ok) => {
      if (!ok) return;
      setSelected({ rel: to, dir: entry.dir });
      if (dir) unfold(dir);
    });
  };

  // Drop-target handlers for folder `dir` ("" = root). Hovering a folded
  // folder while dragging unfolds it after a moment.
  const dropProps = (dir: string) => ({
    onDragOver: (e: React.DragEvent) => {
      if (!e.dataTransfer.types.includes(PATH_MIME) || !canDrop(dragging, dir)) return;
      e.preventDefault();
      e.stopPropagation();
      e.dataTransfer.dropEffect = "move";
      if (dropDir !== dir) {
        setDropDir(dir);
        clearTimeout(unfoldTimer.current);
        if (dir && folded.has(dir)) unfoldTimer.current = setTimeout(() => unfold(dir), 700);
      }
    },
    onDragLeave: (e: React.DragEvent) => {
      if (!e.currentTarget.contains(e.relatedTarget as Node | null) && dropDir === dir) setDropDir(null);
    },
    onDrop: (e: React.DragEvent) => {
      if (!e.dataTransfer.types.includes(PATH_MIME)) return;
      e.preventDefault();
      e.stopPropagation();
      clearTimeout(unfoldTimer.current);
      moveInto(dir);
    },
  });

  /** What a row carries onto the diagram besides its path: a prompt (.md) or a schema (.json). */
  type As = "prompt" | "schema" | null;
  const dragProps = (entry: Entry, abs: string, as: As) => ({
    draggable: renaming !== entry.rel,
    onDragStart: (e: React.DragEvent) => {
      dragging = entry;
      e.dataTransfer.setData(PATH_MIME, entry.rel);
      // A Markdown file is also a prompt the diagram accepts, a JSON file a schema.
      if (as) {
        e.dataTransfer.setData(DRAG_MIME, "agent");
        if (as === "prompt") e.dataTransfer.setData(PROMPT_MIME, JSON.stringify({ md: abs }));
        else e.dataTransfer.setData(SCHEMA_MIME, JSON.stringify({ json: abs }));
      }
      e.dataTransfer.effectAllowed = as ? "copyMove" : "move";
    },
    onDragEnd: () => {
      dragging = null;
      setDropDir(null);
      clearTimeout(unfoldTimer.current);
    },
  });

  const rowEvents = (entry: Entry) => ({
    onContextMenu: (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      setSelected(entry);
      setRowMenu({ at: pointerAt(e), entry });
    },
  });

  const onTreeKey = (e: React.KeyboardEvent) => {
    if (!selected || renaming) return;
    if ((e.target as HTMLElement).tagName === "INPUT") return;
    if (e.key === "F2") {
      e.preventDefault();
      setRenaming(selected.rel);
    } else if (e.key === "Delete" || (e.key === "Backspace" && (e.metaKey || e.ctrlKey))) {
      e.preventDefault();
      remove(selected);
    }
  };

  const rows: ReactElement[] = [];
  const walk = (nodes: TreeNode[], depth: number) => {
    for (const n of nodes) {
      const indent = Array.from({ length: depth }, (_, i) => <span key={i} className="tree-guide" />);
      const entry: Entry = { rel: n.path, dir: n.kind === "dir" };
      const isSel = selected?.rel === n.path;
      const label =
        renaming === n.path ? (
          <RenameInput rel={n.path} onDone={(next) => rename(entry, next)} />
        ) : (
          <span className="tree-name">{n.name}</span>
        );
      if (n.kind === "dir") {
        const open = !folded.has(n.path);
        rows.push(
          <div
            key={`d:${n.path}`}
            className={`tree-row dir ${isSel ? "selected" : ""} ${dropDir === n.path ? "drop-into" : ""}`}
            role="treeitem"
            aria-expanded={open}
            title={n.path}
            {...rowEvents(entry)}
            {...dropProps(n.path)}
          >
            <button
              className="tree-main"
              onClick={() => {
                setSelected(entry);
                toggle(n.path);
              }}
              {...dragProps(entry, joinPath(root, n.path), null)}
            >
              {indent}
              <span className="tree-chevron">{open ? "▾" : "▸"}</span>
              <FolderIcon open={open} />
              {label}
            </button>
            <button
              className="icon-btn tree-action"
              title={`New file or folder in ${n.path}/…`}
              aria-haspopup="menu"
              onClick={(e) => setMenu({ at: anchorOf(e.currentTarget), dir: n.path })}
            >
              +
            </button>
          </div>,
        );
        if (open) walk(n.children, depth + 1);
      } else {
        const abs = joinPath(root, n.path);
        const md = isMarkdown(n.path);
        const json = isJson(n.path);
        rows.push(
          <div
            key={`f:${n.path}`}
            className={`tree-row file ${n.path === activeRel ? "active" : ""} ${isSel ? "selected" : ""}`}
            role="treeitem"
            aria-selected={isSel}
            {...rowEvents(entry)}
            // Dropping onto a file drops into the folder holding it.
            {...dropProps(parentOf(n.path))}
          >
            <button
              className="tree-main"
              onClick={() => {
                setSelected(entry);
                void openFile(abs);
              }}
              title={
                md
                  ? `${n.path}\nDrag onto the diagram to run it as a prompt, or onto a folder to move it.`
                  : json
                    ? `${n.path}\nDrag onto a step to shape its agent's answer (schema:), or onto a folder to move it.`
                    : n.path
              }
              {...dragProps(entry, abs, md ? "prompt" : json ? "schema" : null)}
            >
              {indent}
              <span className="tree-chevron" />
              {md ? <MarkdownIcon /> : json ? <JsonIcon /> : <img className="tree-icon" src={fileIcon} alt="" />}
              {label}
              {dirty[abs] && <span className="tree-dirty" title="unsaved changes">●</span>}
            </button>
          </div>,
        );
      }
    }
  };
  walk(tree, 0);

  return (
    <div className="file-tree" onKeyDown={onTreeKey}>
      <div className={`panel-head ${dropDir === "" ? "drop-into" : ""}`} {...dropProps("")}>
        <span title={root}>{root.split(/[\\/]/).pop()}</span>
        <span className="spacer" />
        <button
          className="icon-btn"
          title="New file or folder…"
          aria-haspopup="menu"
          aria-expanded={!!menu && menu.dir === ""}
          onClick={(e) => {
            // Measure now: React clears e.currentTarget once the handler returns,
            // and a setState updater runs later, during render.
            const at = anchorOf(e.currentTarget);
            setMenu((m) => (m ? null : { at, dir: "" }));
          }}
        >
          +
        </button>
        {tree.some((n) => n.kind === "dir") && (
          <button className="icon-btn" title="Collapse all folders" onClick={() => setFolded(new Set(allDirs(tree)))}>
            ⊟
          </button>
        )}
        <button className="icon-btn" title="Refresh" onClick={() => void refreshFiles()}>
          ⟳
        </button>
      </div>
      {menu && <NewFileMenu at={menu.at} title={menu.dir ? `New in ${menu.dir}/` : "New"} onPick={(item) => startNew(item, menu.dir)} onClose={() => setMenu(null)} />}
      {agentPick && (
        <AgentPicker
          at={agentPick.at}
          title={agentPick.dir ? `New agent in ${agentPick.dir}/` : "New agent — pick a CLI and a role"}
          onClose={() => setAgentPick(null)}
          onPick={(backend, role) => {
            const dir = agentPick.dir;
            setAgentPick(null);
            startNew({ template: { backend, role } }, dir);
          }}
        />
      )}
      {rowMenu && (
        <RowMenu
          at={rowMenu.at}
          entry={rowMenu.entry}
          onClose={() => setRowMenu(null)}
          onNew={() => setMenu({ at: rowMenu.at, dir: rowMenu.entry?.rel ?? "" })}
          onRename={() => rowMenu.entry && setRenaming(rowMenu.entry.rel)}
          onDelete={() => rowMenu.entry && remove(rowMenu.entry)}
        />
      )}
      {creating && (
        <form
          className="new-file"
          onSubmit={(e) => {
            e.preventDefault();
            const item = creating.item;
            const done =
              item === "folder" ? newFolder(name) : isAgentChoice(item) ? newAgentFile(name, item.template.backend, item.template.role) : newFile(name, item.kind);
            void done.then(() => {
              setCreating(null);
              setName("");
            });
          }}
        >
          <span className="new-file-kind">
            {creating.item === "folder"
              ? "New folder — path (Enter to create, Esc to cancel)"
              : isAgentChoice(creating.item)
                ? `⚙ New ${BACKENDS.find((b) => b.id === (creating.item as AgentChoice).template.backend)!.label} ${ROLES.find((r) => r.id === (creating.item as AgentChoice).template.role)!.label.toLowerCase()} — file name (Enter to create, Esc to cancel)`
                : `${creating.item.icon} New ${creating.item.label.toLowerCase()} — file name, folders allowed (Enter to create, Esc to cancel)`}
          </span>
          <input
            autoFocus
            value={name}
            onFocus={(e) => {
              const v = e.target.value;
              const ext = v.search(/(\.prompt\.md|\.schema\.json|\.mh)$/);
              e.target.setSelectionRange(v.lastIndexOf("/") + 1, ext < 0 ? v.length : ext);
            }}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Escape" && setCreating(null)}
            onBlur={() => !name.trim() && setCreating(null)}
          />
        </form>
      )}
      {files.length === 0 && dirs.length === 0 && <p className="muted small pad">No .mh or .md files here yet.</p>}
      <div
        className={`tree ${dropDir === "" ? "drop-into" : ""}`}
        role="tree"
        {...dropProps("")}
        onContextMenu={(e) => {
          e.preventDefault();
          setRowMenu({ at: pointerAt(e), entry: null });
        }}
      >
        {rows}
      </div>
    </div>
  );
}
