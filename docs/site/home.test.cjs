const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const lessons = require('./playground-lessons.js');

const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
const decode = text => text.replace(/&(amp|lt|gt|quot|#39);/g, (_, entity) =>
  ({ amp: '&', lt: '<', gt: '>', quot: '"', '#39': "'" })[entity]);
const examples = new Map([...html.matchAll(/<code id="([^"]+)" class="language-mhl">([\s\S]*?)<\/code>/g)]
  .map(([, id, source]) => [id, decode(source)]));

test('homepage links, copy targets and example tabs point to existing content', () => {
  const ids = [...html.matchAll(/\bid="([^"]+)"/g)].map(match => match[1]);
  assert.equal(new Set(ids).size, ids.length, 'IDs must be unique');
  for (const [, id] of html.matchAll(/\b(?:data-copy|aria-controls|aria-labelledby)="([^"]+)"/g)) {
    assert.ok(ids.includes(id), 'Missing target: ' + id);
  }
  const lessonIds = new Set(lessons.map(lesson => lesson.id));
  for (const [, href] of html.matchAll(/\b(?:href|src)="([^"]+)"/g)) {
    if (/^[a-z]+:/i.test(href)) continue;
    const [file, fragment] = href.split('#');
    assert.ok(!file || fs.existsSync(path.join(__dirname, file)), href);
    if (!fragment) continue;
    if (!file) assert.ok(ids.includes(fragment), href);
    else if (/^playground(?:-en)?\.html$/.test(file)) assert.ok(lessonIds.has(fragment), href);
    else {
      const target = fs.readFileSync(path.join(__dirname, file), 'utf8');
      assert.ok(target.includes('id="' + fragment + '"'), href);
    }
  }
});

// Integration validates the actual source visitors copy, with all state isolated in temp files.
test('homepage programs lint and demonstrate routing, max, fan-out, parallel, resume and contracts',
  { skip: !process.env.MHL_BIN }, () => {
    const binary = path.resolve(process.env.MHL_BIN);
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'mhl-home-test-'));
    function invoke(args) {
      return spawnSync(binary, args, { cwd: directory, encoding: 'utf8', timeout: 15000 });
    }
    function success(args) {
      const result = invoke(args);
      assert.equal(result.status, 0, args.join(' ') + '\n' + result.stdout + result.stderr);
      return result.stdout;
    }
    const file = id => path.join(directory, id + '.mh');
    try {
      assert.ok(examples.size > 0, 'No homepage examples found');
      for (const [id, source] of examples) {
        fs.writeFileSync(file(id), source + '\n');
        assert.match(success(['lint', file(id)]), /No problems found/, id);
      }

      assert.match(success(['run', file('hello-code')]), /Hello, Ana!/);
      const brief = success(['run', file('workflow-code'), '--input', 'artifact=brief']);
      assert.match(brief, /brief ready/);
      assert.doesNotMatch(brief, /adr ready/);
      const adr = success(['run', file('workflow-code'), '--input', 'artifact=adr']);
      assert.match(adr, /adr ready/);
      assert.doesNotMatch(adr, /brief ready/);
      const unknown = invoke(['run', file('workflow-code'), '--input', 'artifact=unknown']);
      assert.notEqual(unknown.status, 0);
      assert.match(unknown.stdout + unknown.stderr, /Unknown artifact/);

      const earlyStop = success(['run', file('limits-code')]);
      assert.match(earlyStop, /round 2/);
      assert.doesNotMatch(earlyStop, /round 3/);
      // Without early convergence, the declared ceiling must stop iteration four.
      fs.writeFileSync(file('ceiling'), examples.get('limits-code').replace('round == 2', 'round == 99'));
      const ceiling = success(['run', file('ceiling')]);
      assert.match(ceiling, /round 3/);
      assert.doesNotMatch(ceiling, /round 4/);

      const fanout = success(['run', file('fanout-code')]);
      for (const name of ['api.go', 'auth.go', 'cli.go']) assert.ok(fanout.includes('check ' + name));
      for (const policy of ['any', '2 of']) {
        fs.writeFileSync(file('wait-policy'), examples.get('fanout-code')
          .replace('wait checks', 'wait ' + policy + ' checks')
          .replace(' on_error: "collect"', ''));
        assert.match(success(['run', file('wait-policy')]), /check (api|auth|cli)\.go/);
      }
      assert.match(success(['run', file('parallel-code')]), /docs ready; tests ready/);

      const approvalArgs = ['run', file('approval-code'), '--session', 'homepage-approval'];
      const paused = success(approvalArgs);
      assert.match(paused, /paused/);
      assert.doesNotMatch(paused, /step: Publish/);
      const resumed = success([...approvalArgs, '--resume', '--input', 'approved=true']);
      assert.match(resumed, /step: Publish/);
      assert.match(resumed, /\napproved\n/);
      assert.match(success(['test', file('contracts-code')]), /2 passed, 0 failed/);

      // Exercise the full hero flow using a deterministic local agent, never a paid provider.
      const localHero = examples.get('hero-code')
        .replace('command: "claude"', 'command: "echo"')
        .replace('args: ["-p", "${prompt}"]', 'args: ["${prompt}"]');
      fs.writeFileSync(file('local-hero'), localHero);
      const hero = success(['run', file('local-hero')]);
      assert.equal((hero.match(/step: Improve/g) || []).length, 3);
      assert.equal((hero.match(/step: Review/g) || []).length, 3);
      assert.match(hero, /Feedback: Review/);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
