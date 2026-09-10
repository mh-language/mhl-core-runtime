const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const core = require("./playground-core.js");
const lessons = require("./playground-lessons.js");
const englishLessons = require("./playground-lessons.en.js")(lessons);

test("English lessons preserve contracts and IDs while translating learning material", () => {
  assert.equal(englishLessons.length, lessons.length);
  for (const [index, english] of englishLessons.entries()) {
    const portuguese = lessons[index];
    for (const key of ["id", "file", "mode", "args", "source", "resume", "external"]) {
      assert.equal(english[key], portuguese[key], english.id + ": " + key);
    }
    for (const key of ["title", "description", "group", "challenge", "requirements"]) {
      assert.notEqual(english[key], portuguese[key], english.id + ": " + key);
    }
    assert.equal(english.notes.length, portuguese.notes.length);
    assert.deepEqual(core.diagnostics(english.code, "en"), []);
  }
  assert.match(lessons[0].code, /tentativa/); // Translating does not mutate Portuguese lessons.
  assert.match(englishLessons[0].code, /attempt/);
});

test("diagnostics and draft storage support both languages without overwriting existing state", () => {
  assert.equal(core.diagnostics("", "en")[0].message, "The editor is empty.");
  assert.equal(core.diagnostics('var x = "oops', "en")[0].message, "Unclosed string.");
  assert.match(core.diagnostics("}", "en")[0].message, /Unexpected delimiter/);
  assert.match(core.diagnostics("{", "en")[0].message, /Unclosed delimiter/);
  const data = {
    "mhl-playground-v2": JSON.stringify({current: "valores", drafts: {valores: "Portuguese draft"}}),
    "mhl-playground-v2-en": JSON.stringify({current: "valores", drafts: {valores: "English draft"}})
  };
  const storage = {getItem: key => data[key]};
  assert.equal(core.readState(storage, lessons).drafts.valores, "Portuguese draft");
  assert.equal(core.readState(storage, englishLessons, "mhl-playground-v2-en").drafts.valores, "English draft");
});

test("highlight preserves source and isolates comments, strings, durations and markup", () => {
  const source = '// wait 30s <img>\nvar s = "https://example.com/if?x=1&y=2"\nwait tasks timeout: 500ms';
  const tokens = core.tokenize(source);
  assert.equal(tokens.map(token => token.value).join(""), source);
  assert.equal(tokens[0].kind, "comment");
  assert.equal(tokens.find(token => token.value === "500ms").kind, "number");
  const html = core.highlight(source);
  assert.match(html, /tok-comment">\/\/ wait 30s &lt;img&gt;<\/span>/);
  assert.match(html, /tok-string">&quot;https:\/\/example.com\/if\?x=1&amp;y=2&quot;<\/span>/);
  assert.ok(!html.includes("<img>"));
  assert.equal((html.match(/<span/g) || []).length, (html.match(/<\/span>/g) || []).length);
});

test("multiline and escaped strings do not produce false bracket diagnostics", () => {
  const source = 'prompt P() { """\nstring { [ "quoted"\n""" }\nvar s = "escaped \\" [ }" // (';
  assert.deepEqual(core.diagnostics(source), []);
  assert.equal(core.tokenize(source).filter(token => token.kind === "string").length, 2);
});

test("diagnostics locate broken closures without claiming semantic validation", () => {
  const errors = core.diagnostics("pipeline A {\n step X { log(] } }");
  assert.ok(errors.some(error => error.line === 2 && error.column === 15 && error.message.includes("]")));
  assert.ok(errors.some(error => error.line === 1 && error.message.includes("fechamento")));
  assert.match(core.diagnostics('var s = "oops')[0].message, /String/);
  assert.match(core.diagnostics("/* oops")[0].message, /Comentário/);
  assert.equal(core.diagnostics("").length, 1);
  assert.deepEqual(core.diagnostics("not valid MHL"), []);
});

test("stored progress is robust to denied access, invalid JSON and stale lesson IDs", () => {
  const empty = {current: lessons[0].id, completed: [], drafts: {}};
  assert.deepEqual(core.readState(undefined, lessons), empty);
  assert.deepEqual(core.readState({getItem() { throw Error("denied"); }}, lessons), empty);
  assert.deepEqual(core.readState({getItem: () => "{"}, lessons), empty);
  const stored = {current: "gone", completed: ["valores", "gone", "valores"], drafts: {valores: "", gone: "stale", lacos: 42}};
  assert.deepEqual(core.readState({getItem: () => JSON.stringify(stored)}, lessons),
    {...empty, completed: ["valores"], drafts: {valores: ""}});
});

test("every lesson has unique identity, complete source and actionable learning material", () => {
  assert.equal(new Set(lessons.map(lesson => lesson.id)).size, lessons.length);
  for (const lesson of lessons) {
    assert.deepEqual(core.diagnostics(lesson.code), [], lesson.id);
    assert.ok(lesson.challenge && lesson.notes.length && lesson.requirements, lesson.id);
    assert.ok(fs.existsSync(path.resolve(__dirname, "../..", lesson.source)), lesson.source);
    assert.equal(core.tokenize(lesson.code).map(token => token.value).join(""), lesson.code);
  }
});

// Optional integration: MHL_BIN=/absolute/path/to/mhl node --test docs/site/playground.test.cjs
// Generated lesson files and all runtime state remain in a disposable temporary directory.
for (const [language, lessons] of Object.entries({pt: require("./playground-lessons.js"), en: englishLessons})) {
test(language + ": all lessons lint; local examples run; limits, timeout and resume behave as taught",
  { skip: !process.env.MHL_BIN }, () => {
    const binary = path.resolve(process.env.MHL_BIN);
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "mhl-playground-test-"));
    const invoke = args => spawnSync(binary, args, {cwd: directory, encoding: "utf8", timeout: 15000});
    const assertSuccess = (result, label) => assert.equal(result.status, 0, label + "\n" + result.stdout + result.stderr);
    try {
      for (const lesson of lessons) {
        fs.writeFileSync(path.join(directory, lesson.file), lesson.code);
        assertSuccess(invoke(["lint", lesson.file]), lesson.id + " lint");
        if (lesson.external) continue; // Never contact services or resolve external credentials in tests.
        const args = lesson.args ? lesson.args.split(" ") : [];
        if (lesson.mode === "run") assertSuccess(invoke(["run", lesson.file, ...args, "--dry-run"]), lesson.id + " dry-run");
        const result = invoke([lesson.mode, lesson.file, ...args]);
        if (lesson.id === "retomada") {
          assert.match(result.stdout + result.stderr, /paus|suspend|Aguardando|Awaiting/i);
          const resumed = invoke(["run", lesson.file, "--resume", "--input", "approved=true"]);
          assertSuccess(resumed, "resume");
          assert.match(resumed.stdout, /approved/);
        } else {
          assertSuccess(result, lesson.id + " run");
          const expected = {
            valores: language === "en" ? "mhl: attempt 2 of 3" : "mhl: tentativa 2 de 3", colecoes: "team: lint, deploy",
            decisoes: language === "en" ? "passed" : "aprovado",
            lacos: language === "en" ? "total=8; attempts=2" : "total=8; tentativas=2", pipeline: "manual review",
            agentes: "Review api.go. Focus on security.", memoria: "reviewed",
            "loop-max": language === "en" ? "round 3" : "rodada 3", "loop-condicao": "check 3: done", parallel: "docs ready; issues ready"
          }[lesson.id];
          if (expected) assert.ok(result.stdout.includes(expected), lesson.id + "\n" + result.stdout);
        }
      }
      const loop = lessons.find(lesson => lesson.id === "loop-condicao");
      fs.writeFileSync(path.join(directory, "limited.mh"), loop.code.replace("checks >= 3", "checks >= 8"));
      const limited = invoke(["run", "limited.mh"]);
      assertSuccess(limited, "iteration ceiling");
      assert.match(limited.stdout, /check 5: pending/);
      assert.doesNotMatch(limited.stdout, /check 6:/);
      const timeout = lessons.find(lesson => lesson.id === "timeout");
      fs.writeFileSync(path.join(directory, "deadline.mh"), timeout.code.replace("timeout 2s", "timeout 50ms").replace("time.sleep(20ms)", "time.sleep(300ms)"));
      const expired = invoke(["run", "deadline.mh"]);
      assert.notEqual(expired.status, 0);
      assert.match(expired.stdout + expired.stderr, /timeout|deadline/i);
      assert.doesNotMatch(expired.stdout, /finished/);
    } finally {
      fs.rmSync(directory, {recursive: true, force: true});
    }
  });
}
