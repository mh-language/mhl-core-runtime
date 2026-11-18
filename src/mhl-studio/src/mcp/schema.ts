// A tool's `inputSchema` (JSON Schema, as `mhl serve mcp` derives it from a
// workflow's inputs — runtime.Pipeline.InputSchema) turned into form fields,
// and the typed-in strings turned back into `arguments`. A blank field is
// left out, so the server applies the input's default.

export type FieldKind = "string" | "number" | "integer" | "boolean" | "enum" | "json";

export interface SchemaField {
  name: string;
  kind: FieldKind;
  required: boolean;
  /** The schema's type, for display (`string`, `object`, `string[]`, …). */
  type: string;
  def?: unknown;
  description?: string;
  variants?: string[];
}

type Schema = {
  type?: string | string[];
  enum?: unknown[];
  items?: Schema;
  default?: unknown;
  description?: string;
  properties?: Record<string, Schema>;
  required?: string[];
};

function baseType(s: Schema): string | undefined {
  const t = Array.isArray(s.type) ? s.type.find((x) => x !== "null") : s.type;
  return t;
}

function displayType(s: Schema): string {
  const t = baseType(s);
  if (t === "array") return `${s.items ? displayType(s.items) : "any"}[]`;
  if (s.enum) return "enum";
  return t ?? "any";
}

export function schemaFields(schema: unknown): SchemaField[] {
  const s = (schema ?? {}) as Schema;
  const required = new Set(s.required ?? []);
  return Object.entries(s.properties ?? {}).map(([name, p]) => {
    const t = baseType(p);
    let kind: FieldKind = "json";
    if (p.enum && p.enum.every((v) => typeof v === "string")) kind = "enum";
    else if (t === "string" || t === "number" || t === "integer" || t === "boolean") kind = t;
    return {
      name,
      kind,
      required: required.has(name),
      type: displayType(p),
      def: p.default,
      description: p.description,
      variants: kind === "enum" ? (p.enum as string[]) : undefined,
    };
  });
}

/** What a blank field means, as its placeholder. */
export function placeholder(f: SchemaField): string {
  if (f.def !== undefined) return `default: ${JSON.stringify(f.def)}`;
  if (f.required) return "required";
  return f.kind === "json" ? "JSON" : "";
}

export interface Built {
  args: Record<string, unknown>;
  /** A field's problem, by field name. */
  errors: Record<string, string>;
}

/** The `arguments` object for the form's values. */
export function buildArguments(fields: SchemaField[], values: Record<string, string>): Built {
  const args: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  for (const f of fields) {
    const raw = values[f.name] ?? "";
    if (raw.trim() === "") {
      if (f.required && f.def === undefined) errors[f.name] = "required";
      continue;
    }
    switch (f.kind) {
      case "string":
      case "enum":
        args[f.name] = raw;
        break;
      case "number":
      case "integer": {
        const n = Number(raw.trim());
        if (!Number.isFinite(n)) errors[f.name] = "not a number";
        else if (f.kind === "integer" && !Number.isInteger(n)) errors[f.name] = "not an integer";
        else args[f.name] = n;
        break;
      }
      case "boolean":
        if (raw === "true" || raw === "false") args[f.name] = raw === "true";
        else errors[f.name] = "true or false";
        break;
      case "json":
        try {
          args[f.name] = JSON.parse(raw);
        } catch {
          errors[f.name] = "not valid JSON";
        }
        break;
    }
  }
  return { args, errors };
}

/** The form values that reproduce `args` (to edit a previous call's arguments again). */
export function valuesFor(fields: SchemaField[], args: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const f of fields) {
    const v = args[f.name];
    if (v === undefined) continue;
    out[f.name] = f.kind === "json" ? JSON.stringify(v) : String(v);
  }
  return out;
}
