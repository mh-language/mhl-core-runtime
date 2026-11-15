// A `timeout <duration>` clause as the inspector edits it: a positive whole
// number and a unit. The language's Duration token is `[0-9]+(ms|s|m|h|d)`
// (internal/lang/parser/lexer.go) and lint rejects zero.

export const UNITS = ["h", "m", "s", "ms"] as const;
export type Unit = (typeof UNITS)[number] | "d";

export interface Duration {
  amount: string;
  unit: Unit;
}

/** Splits `30s` into {amount: "30", unit: "s"}; null for anything else (or none). */
export function parseDuration(text: string | undefined): Duration | null {
  const m = /^(\d+)(ms|s|m|h|d)$/.exec((text ?? "").trim());
  return m ? { amount: String(Number(m[1])), unit: m[2] as Unit } : null;
}

/** Keeps only digits, without leading zeros — what the amount box accepts. */
export function cleanAmount(text: string): string {
  return text.replace(/\D/g, "").replace(/^0+/, "");
}

/** The clause value for amount + unit; "" (no timeout) when the amount is empty or zero. */
export function formatDuration(amount: string, unit: Unit): string {
  const n = cleanAmount(amount);
  return n ? `${n}${unit}` : "";
}
