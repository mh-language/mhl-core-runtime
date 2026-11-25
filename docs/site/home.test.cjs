const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const vm = require('node:vm');
const { spawnSync } = require('node:child_process');
const lessons = require('./playground-lessons.js');

const read = file => fs.readFileSync(path.join(__dirname, file), 'utf8');
const newPages = ['index.html', 'start.html', 'examples.html', 'docs.html', 'why-mhl.html'];
const idsIn = html => [...html.matchAll(/\bid="([^"]+)"/g)].map(match => match[1]);
const decode = text => text.replace(/&(amp|lt|gt|quot|#39);/g, (_, entity) =>
  ({ amp: '&', lt: '<', gt: '>', quot: '"', '#39': "'" })[entity]);
const examples = new Map(newPages.flatMap(file => [...read(file).matchAll(/<code id="([^"]+)" class="language-mhl">([\s\S]*?)<\/code>/g)])
  .map(([, id, source]) => [id, decode(source)]));

test('page links, copy targets and example anchors point to existing content', () => {
  const lessonIds = lessons.map(lesson => lesson.id);
  for (const page of newPages) {
    const html = read(page);
    const ids = idsIn(html);
    assert.equal(new Set(ids).size, ids.length, 'IDs must be unique in ' + page);
    for (const [, id] of html.matchAll(/\b(?:data-copy|aria-controls|aria-labelledby)="([^"]+)"/g)) {
      assert.ok(ids.includes(id), page + ' missing target: ' + id);
    }
    const links = [...html.matchAll(/\b(?:href|src)="([^"]+)"/g)].map(([, url]) => url);
    for (const [, srcset] of html.matchAll(/\bsrcset="([^"]+)"/g)) {
      links.push(...srcset.split(',').map(candidate => candidate.trim().split(/\s+/)[0]));
    }
    for (const href of links) {
      if (/^[a-z]+:/i.test(href)) continue;
      const [file, fragment] = href.split('#');
      assert.ok(!file || fs.existsSync(path.join(__dirname, file)), page + ' → ' + href);
      if (!fragment) continue;
      const targets = /^playground(?:-en)?\.html$/.test(file) ? lessonIds : idsIn(read(file || page));
      assert.ok(targets.includes(fragment), page + ' → ' + href);
    }
  }
});

test('all public documentation and learning pages use the same primary navigation', () => {
  const pages = [...newPages, 'download.html', 'playground.html', 'playground-en.html',
    ...['Reference', 'Specification', 'Extensions', 'Servers', 'Studio'].map(name => 'Docs-' + name + '.dc.html')];
  for (const page of pages) {
    const menu = read(page).match(/<nav class="site-menu"[^>]*>([\s\S]*?)<\/nav>/)?.[1];
    assert.ok(menu, page + ' is missing the primary navigation');
    assert.deepEqual([...menu.matchAll(/href="([^"]+)"/g)].map(([, href]) => href),
      ['start.html', 'examples.html', 'docs.html', 'download.html'], page);
  }
});

test('previous homepage fragments still open the corresponding content', () => {
  const script = read('home-redirects.js');
  const moved = {
    '#start': 'start.html',
    '#examples': 'examples.html',
    '#docs': 'docs.html',
    '#why': 'why-mhl.html',
    '#hero-code': 'examples.html#refine-code',
    '#choose-code': 'examples.html#panel-workflow',
    '#hello-code': 'start.html#hello-code',
    '#tab-limits': 'examples.html#panel-limits',
    '#panel-approval': 'examples.html#panel-approval',
    '#parallel-code': 'examples.html#panel-parallel',
    '#top': undefined,
    '#main': undefined,
  };
  for (const [hash, target] of Object.entries(moved)) {
    let actual;
    vm.runInNewContext(script, {
      location: { hash, replace: url => { actual = url; } },
      window: { addEventListener() {} },
    });
    assert.equal(actual, target, hash);
    if (actual) {
      const [page, fragment] = actual.split('#');
      const html = read(page);
      if (fragment) assert.ok(idsIn(html).includes(fragment), actual);
    }
  }
});

// Integration validates the actual source visitors copy, with all state isolated in temp files.
test('site programs lint and demonstrate routing, max, fan-out, parallel, resume and contracts',
  { skip: !process.env.MHL_BIN }, () => {
    const binary = path.resolve(process.env.MHL_BIN);
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'mhl-home-test-'));
    function invoke(args, env) {
      return spawnSync(binary, args, {
        cwd: directory, encoding: 'utf8', timeout: 15000, env: { ...process.env, ...env },
      });
    }
    function success(args, env) {
      const result = invoke(args, env);
      assert.equal(result.status, 0, args.join(' ') + '\n' + result.stdout + result.stderr);
      return result.stdout;
    }
    const file = id => path.join(directory, id + '.mh');
    try {
      assert.equal(examples.size, 8, 'A workflow example is missing');
      for (const [id, source] of examples) {
        fs.writeFileSync(file(id), source + '\n');
        assert.match(success(['lint', file(id)]), /No problems found/, id);
        const inputs = id === 'contracts-code' ? ['--input', 'name=Ana'] : [];
        assert.match(success(['run', file(id), '--dry-run', ...inputs]), /No step ran/, id);
      }

      assert.match(success(['run', file('hello-code')]), /Hello, Ana!/);
      const brief = success(['run', file('workflow-code'), '--input', 'artifact=Brief']);
      assert.match(brief, /brief ready/);
      assert.doesNotMatch(brief, /adr ready/);
      const adr = success(['run', file('workflow-code'), '--input', 'artifact=Adr']);
      assert.match(adr, /adr ready/);
      assert.doesNotMatch(adr, /brief ready/);
      const unknown = invoke(['run', file('workflow-code'), '--dry-run', '--input', 'artifact=unknown']);
      assert.notEqual(unknown.status, 0);
      assert.match(unknown.stdout + unknown.stderr, /not a variant of enum ArtifactKind/);

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
          .replace(' collect', ''));
        assert.match(success(['run', file('wait-policy')]), /check (api|auth|cli)\.go/);
      }
      fs.writeFileSync(path.join(directory, 'check.cjs'), `
        if (process.argv[2] === 'check auth.go') {
          console.error('simulated failure');
          process.exit(1);
        }
        console.log(process.argv[2]);
      `);
      fs.writeFileSync(file('partial-failure'), examples.get('fanout-code')
        .replace('command: "echo"', 'command: env("MHL_TEST_NODE_CMD")')
        .replace('args: ["${prompt}"]', 'args: ["check.cjs", "${prompt}"]'));
      const partial = success(['run', file('partial-failure')], { MHL_TEST_NODE_CMD: process.execPath });
      assert.match(partial, /check api\.go/);
      assert.match(partial, /check cli\.go/);
      assert.match(partial, /simulated failure/);

      assert.match(success(['run', file('parallel-code')]), /docs ready; tests ready/);

      const approvalArgs = ['run', file('approval-code'), '--session', 'homepage-approval'];
      const paused = success(approvalArgs);
      assert.match(paused, /paused/);
      assert.match(paused, /awaiting a decision of type bool/);
      assert.doesNotMatch(paused, /step: (Publish|Reject)/);
      const invalid = invoke([...approvalArgs, '--resume', '--decision', 'nope']);
      assert.notEqual(invalid.status, 0);
      assert.match(invalid.stderr, /not a valid bool/);
      const resumed = success([...approvalArgs, '--resume', '--decision', 'true']);
      assert.match(resumed, /step: Publish/);
      assert.match(resumed, /\napproved\n/);
      assert.doesNotMatch(resumed, /step: Reject/);
      const rejectArgs = ['run', file('approval-code'), '--session', 'homepage-rejection'];
      assert.match(success(rejectArgs), /paused/);
      const rejected = success([...rejectArgs, '--resume', '--decision', 'false']);
      assert.match(rejected, /\nrejected\n/);
      assert.doesNotMatch(rejected, /step: Publish/);
      assert.match(success(['test', file('contracts-code')]), /3 passed, 0 failed/);

      const refine = JSON.parse(success(['run', file('refine-code'), '--format', 'json'], { MHL_WRITER_CMD: 'echo' }));
      assert.equal(refine.iterations, 3);
      assert.deepEqual(Object.keys(refine.vars), ['draft']);
      assert.match(refine.vars.draft, /Review the draft for clarity/);
      assert.match(refine.vars.draft, /Review the draft for consistency/);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
