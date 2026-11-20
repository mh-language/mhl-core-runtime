// The decision form a paused run's gate asks for — `pause(..., decision: T)`
// reports `awaiting.schema`, T as JSON Schema (execsvc.Awaiting). These turn
// that schema into form fields and the filled-in fields back into the
// decision value `mhl run --resume --decision` takes.

export type FieldKind = "enum" | "bool" | "number" | "string" | "json";

export interface DecisionField {
  /** Property name; "" when the decision itself is a scalar. */
  name: string;
  kind: FieldKind;
  required: boolean;
  options?: string[];
}

type Schema = { type?: string; enum?: unknown[]; properties?: Record<string, Schema>; required?: string[] };

function kindOf(s: Schema): Pick<DecisionField, "kind" | "options"> {
  if (Array.isArray(s.enum)) return { kind: "enum", options: s.enum.map(String) };
  if (s.type === "boolean") return { kind: "bool" };
  if (s.type === "number" || s.type === "integer") return { kind: "number" };
  if (s.type === "string") return { kind: "string" };
  return { kind: "json" };
}

/** The fields of a decision of type `schema`: one per property of an object, else one for the value itself. */
export function decisionFields(schema: unknown): DecisionField[] {
  const s = (schema ?? {}) as Schema;
  if (s.type === "object" && s.properties) {
    const req = new Set(s.required ?? []);
    return Object.entries(s.properties).map(([name, p]) => ({ name, required: req.has(name), ...kindOf(p) }));
  }
  return [{ name: "", required: true, ...kindOf(s) }];
}

/** A field's starting value: the first variant of an enum, false for a bool, else empty. */
export function initialValue(f: DecisionField): string {
  if (f.kind === "enum") return f.options?.[0] ?? "";
  if (f.kind === "bool") return "false";
  return "";
}

function parse(f: DecisionField, raw: string): unknown {
  switch (f.kind) {
    case "bool":
      return raw === "true";
    case "number": {
      const n = Number(raw);
      if (raw.trim() === "" || Number.isNaN(n)) throw new Error(`${f.name || "the decision"} must be a number`);
      return n;
    }
    case "json":
      try {
        return JSON.parse(raw);
      } catch {
        throw new Error(`${f.name || "the decision"} must be JSON`);
      }
    default:
      return raw;
  }
}

/**
 * The decision the filled-in `values` (by field name) make. An optional
 * field left empty is omitted; a required one is an error.
 */
export function buildDecision(fields: DecisionField[], values: Record<string, string>): unknown {
  if (fields.length === 1 && fields[0].name === "") return parse(fields[0], values[""] ?? "");
  const out: Record<string, unknown> = {};
  for (const f of fields) {
    const raw = values[f.name] ?? "";
    if (raw === "" && f.kind !== "bool") {
      if (f.required) throw new Error(`${f.name} is required`);
      continue;
    }
    out[f.name] = parse(f, raw);
  }
  return out;
}
