#!/usr/bin/env node
// Writes THIRD_PARTY_NOTICES.md: the licenses of everything an mhl studio
// installer distributes —
//   - the npm packages the frontend bundle is built from (production deps),
//   - the Rust crates linked into the app, for every target the release
//     workflow builds (studio-release.yml),
//   - the bundled mhl runtime, whose Go notices are the repository's
//     THIRD_PARTY_NOTICES.md (included verbatim).
// Identical license texts are printed once, after the packages they cover.
// tauri.conf.json bundles the file into every installer.
//
//   node scripts/third-party-notices.mjs           # (re)write the file
//   node scripts/third-party-notices.mjs --check   # fail if it is out of date
//
// Reads node_modules (run `npm ci` first) and the cargo registry; cargo
// downloads any crate source it lacks for a target.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const studio = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repo = resolve(studio, "../..");
const out = join(studio, "THIRD_PARTY_NOTICES.md");
// Keep in sync with the matrix of .github/workflows/studio-release.yml.
const TARGETS = [
  "aarch64-apple-darwin",
  "x86_64-apple-darwin",
  "x86_64-pc-windows-msvc",
  "aarch64-pc-windows-msvc",
  "x86_64-unknown-linux-gnu",
  "aarch64-unknown-linux-gnu",
];
const LICENSE_FILE = /^(licen[cs]e|copying|unlicense|notice)([-._].*)?$/i;

/** The license/notice files in a package directory, sorted, as [name, text]. */
function licenseFiles(dir) {
  return readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isFile() && LICENSE_FILE.test(e.name))
    .map((e) => e.name)
    .sort()
    .map((name) => [name, readFileSync(join(dir, name), "utf8").replace(/\r\n/g, "\n").trim()]);
}

function repoUrl(r) {
  const url = typeof r === "string" ? r : r?.url;
  return url ? url.replace(/^git\+/, "").replace(/\.git$/, "").replace(/^git:\/\//, "https://") : "";
}

// The production packages, from package-lock.json rather than `npm ls`: the
// lockfile is the same on every OS (no production dependency is
// platform-specific), needs no network, and marks what only development
// needs (`dev`, or `devOptional` such as @types/*), which never reaches the
// bundle.
function npmPackages() {
  const lock = JSON.parse(readFileSync(join(studio, "package-lock.json"), "utf8"));
  const dirs = Object.entries(lock.packages)
    .filter(([key, entry]) => key && !entry.dev && !entry.devOptional)
    .map(([key]) => join(studio, key));
  const seen = new Map();
  for (const dir of dirs) {
    const manifest = join(dir, "package.json");
    if (!existsSync(manifest)) throw new Error(`${manifest} is missing — run npm ci first`);
    const pkg = JSON.parse(readFileSync(manifest, "utf8"));
    const id = `${pkg.name}@${pkg.version}`;
    if (seen.has(id)) continue;
    const author = typeof pkg.author === "string" ? pkg.author : pkg.author?.name;
    seen.set(id, {
      name: pkg.name,
      version: pkg.version,
      license: typeof pkg.license === "string" ? pkg.license : pkg.license?.type ?? "UNKNOWN",
      authors: author ? [author] : [],
      repository: repoUrl(pkg.repository) || pkg.homepage || "",
      files: licenseFiles(dir),
    });
  }
  return [...seen.values()];
}

function rustCrates() {
  const tauri = join(studio, "src-tauri");
  const all = new Map();
  for (const target of TARGETS) {
    const meta = JSON.parse(
      execFileSync("cargo", ["metadata", "--format-version", "1", "--locked", "--filter-platform", target], {
        cwd: tauri,
        encoding: "utf8",
        maxBuffer: 256 * 1024 * 1024,
      }),
    );
    const pkgs = new Map(meta.packages.map((p) => [p.id, p]));
    const nodes = new Map(meta.resolve.nodes.map((n) => [n.id, n]));
    // Only what is linked into the app: normal (not build/dev) dependencies.
    const seen = new Set([meta.resolve.root]);
    const todo = [meta.resolve.root];
    while (todo.length) {
      for (const d of nodes.get(todo.pop()).deps) {
        if (d.dep_kinds.some((k) => k.kind === null) && !seen.has(d.pkg)) {
          seen.add(d.pkg);
          todo.push(d.pkg);
        }
      }
    }
    seen.delete(meta.resolve.root);
    for (const id of seen) {
      const p = pkgs.get(id);
      const key = `${p.name}@${p.version}`;
      if (all.has(key)) continue;
      all.set(key, {
        name: p.name,
        version: p.version,
        license: p.license ?? (p.license_file ? `see ${p.license_file}` : "UNKNOWN"),
        authors: p.authors ?? [],
        repository: p.repository ?? p.homepage ?? "",
        files: licenseFiles(dirname(p.manifest_path)),
      });
    }
  }
  return [...all.values()];
}

const byName = (a, b) => (a.name === b.name ? a.version.localeCompare(b.version, "en", { numeric: true }) : a.name.localeCompare(b.name));
const fence = (text) => {
  const ticks = "`".repeat(Math.max(3, ...[...text.matchAll(/`+/g)].map((m) => m[0].length + 1)));
  return `${ticks}text\n${text}\n${ticks}`;
};

/** One ecosystem's section: a table of packages, then each distinct license text once. */
function section(title, intro, packages) {
  packages.sort(byName);
  const texts = new Map(); // hash -> { text, users: [] }
  for (const p of packages)
    for (const [file, text] of p.files) {
      const h = createHash("sha256").update(text).digest("hex").slice(0, 12);
      if (!texts.has(h)) texts.set(h, { text, users: [] });
      texts.get(h).users.push(`${p.name} ${p.version} (${file})`);
    }
  const lines = [`## ${title}`, "", intro, "", "| Package | Version | License | Source |", "| --- | --- | --- | --- |"];
  for (const p of packages) lines.push(`| ${p.name} | ${p.version} | ${p.license.replace(/\|/g, "\\|")} | ${p.repository} |`);
  const bare = packages.filter((p) => p.files.length === 0);
  if (bare.length) {
    lines.push("", "These packages ship no license file; their license is the SPDX identifier above, copyright their authors:", "");
    for (const p of bare) lines.push(`- ${p.name} ${p.version} — ${p.license}${p.authors.length ? ` — ${p.authors.join(", ")}` : ""}`);
  }
  lines.push("", `### ${title}: license texts`, "");
  for (const { text, users } of [...texts.values()].sort((a, b) => a.users[0].localeCompare(b.users[0]))) {
    lines.push(`Used by: ${users.join(", ")}`, "", fence(text), "");
  }
  return lines.join("\n");
}

const goNotices = readFileSync(join(repo, "THIRD_PARTY_NOTICES.md"), "utf8").replace(/^# .*\n+/, "").trim();
const doc = [
  "# Third-party notices — mhl studio",
  "",
  "<!-- Generated by scripts/third-party-notices.mjs (npm run notices). Do not edit by hand. -->",
  "",
  "mhl studio is MIT-licensed (see LICENSE.md). Its installers also distribute the",
  "third-party software below: the frontend's npm packages, the Rust crates linked into",
  "the app, and the bundled `mhl` runtime.",
  "",
  section("npm packages", "Production dependencies of the frontend bundle (package-lock.json, without dev-only packages).", npmPackages()),
  section("Rust crates", `Normal dependencies of the app for ${TARGETS.join(", ")}.`, rustCrates()),
  "## The bundled mhl runtime (Go)",
  "",
  goNotices.replace(/^(#+) /gm, "#$1 "),
  "",
].join("\n");

if (process.argv.includes("--check")) {
  const current = existsSync(out) ? readFileSync(out, "utf8") : "";
  if (current !== doc) {
    console.error("THIRD_PARTY_NOTICES.md is out of date — run: npm run notices");
    process.exit(1);
  }
  console.log("THIRD_PARTY_NOTICES.md is up to date");
} else {
  writeFileSync(out, doc);
  console.log(`wrote ${out}`);
}
