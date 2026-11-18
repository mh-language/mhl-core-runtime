const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { platformFrom, findRelease, studioFiles, cliFiles, PLATFORMS } = require('./download.js');

const html = fs.readFileSync(path.join(__dirname, 'download.html'), 'utf8');
const asset = name => ({ name, size: 12 * 1048576, browser_download_url: 'https://github.com/mh-language/mhl-core-runtime/releases/download/x/' + name });

// The names tauri-action gives the installers studio-release.yml uploads.
const studio = (tag, extra = {}) => ({
  tag_name: tag, draft: false, prerelease: false, published_at: '2026-11-18T00:00:00Z', html_url: 'https://github.com/x/' + tag,
  assets: ['mhl.studio_0.1.0_aarch64.dmg', 'mhl.studio_0.1.0_x64.dmg', 'mhl.studio_0.1.0_x64-setup.exe', 'mhl.studio_0.1.0_x64_en-US.msi',
    'mhl.studio_0.1.0_amd64.AppImage', 'mhl.studio_0.1.0_amd64.deb', 'mhl.studio-0.1.0-1.x86_64.rpm', 'mhl.studio_aarch64.app.tar.gz',
    'mhl.studio_0.1.0_x64-setup.exe.sig'].map(asset),
  ...extra,
});
// The assets of the real v1.11.1 language release.
const language = {
  tag_name: 'v1.11.1', draft: false, prerelease: false, published_at: '2026-11-17T12:09:21Z', html_url: 'https://github.com/x/v1.11.1',
  assets: ['checksums.txt', 'mhl-1.11.1-darwin-arm64.tar.gz', 'mhl-1.11.1-linux-amd64.tar.gz', 'mhl-1.11.1-linux-arm64.tar.gz',
    'mhl-1.11.1-windows-amd64.zip', 'mhl-1.11.1-windows-arm64.zip', 'mhl-language-0.7.2.vsix'].map(asset),
};

test('download page links, copy targets and script hooks point to existing content', () => {
  const ids = [...html.matchAll(/\bid="([^"]+)"/g)].map(m => m[1]);
  assert.equal(new Set(ids).size, ids.length, 'IDs must be unique');
  for (const [, id] of html.matchAll(/\b(?:data-copy|aria-labelledby)="([^"]+)"/g)) assert.ok(ids.includes(id), 'Missing target: ' + id);
  for (const id of ['studio-main', 'studio-main-label', 'studio-meta', 'studio-alt', 'studio-pending', 'os-note', 'cli-unix', 'cli-windows',
    'cli-intel', 'cli-meta', 'cli-archive', 'cli-archive-row', 'cli-checksums', 'vsix-link', 'lang-toggle', 'theme-toggle', 'copy-status',
    'quick-studio', 'quick-studio-label', 'quick-studio-meta', 'quick-or', 'quick-cli-box', 'quick-cli', 'quick-cli-meta']) {
    assert.ok(ids.includes(id), 'download.js needs #' + id);
  }
  for (const [, os] of html.matchAll(/data-os="([^"]+)"/g)) assert.ok(PLATFORMS.includes(os), os);
  for (const [, href] of html.matchAll(/\b(?:href|src)="([^"]+)"/g)) {
    if (/^[a-z]+:/i.test(href)) continue;
    const [file, fragment] = href.split('#');
    assert.ok(!file || fs.existsSync(path.join(__dirname, file)), href);
    if (fragment) assert.ok(fs.readFileSync(path.join(__dirname, file || 'download.html'), 'utf8').includes('id="' + fragment + '"'), href);
  }
});

test('the visitor\'s system is detected from the browser', () => {
  const mac = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15';
  assert.equal(platformFrom({ userAgent: mac, platform: 'MacIntel', arch: 'arm' }), 'mac-arm64');
  assert.equal(platformFrom({ userAgent: mac, platform: 'MacIntel', arch: 'x86' }), 'mac-x64');
  assert.equal(platformFrom({ userAgent: mac, platform: 'MacIntel', renderer: 'Apple GPU' }), 'mac-arm64');
  assert.equal(platformFrom({ userAgent: mac, platform: 'MacIntel', renderer: 'Intel(R) Iris(TM) Plus Graphics' }), 'mac-x64');
  assert.equal(platformFrom({ userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', platform: 'Win32' }), 'win-x64');
  assert.equal(platformFrom({ userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', platform: 'Windows', arch: 'arm' }), 'win-arm64');
  assert.equal(platformFrom({ userAgent: 'Mozilla/5.0 (X11; Linux x86_64)', platform: 'Linux x86_64' }), 'linux-x64');
  assert.equal(platformFrom({ userAgent: 'Mozilla/5.0 (X11; Linux aarch64)', platform: 'Linux aarch64' }), 'linux-arm64');
  assert.equal(platformFrom({ userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)', platform: 'iPhone' }), null);
  assert.equal(platformFrom({ userAgent: 'Mozilla/5.0 (Linux; Android 15; Pixel 9)', platform: 'Linux armv8l' }), null);
});

test('the newest published release of each product is picked', () => {
  const draft = studio('studio-v0.2.0', { draft: true });
  const pre = studio('studio-v0.2.0-rc.1', { prerelease: true });
  assert.equal(findRelease([language], 'studio'), null, 'no studio release yet');
  assert.equal(findRelease([draft, language], 'studio'), null, 'drafts are not public');
  assert.equal(findRelease([pre, studio('studio-v0.1.0'), language], 'studio').tag_name, 'studio-v0.1.0', 'stable first');
  assert.equal(findRelease([pre, language], 'studio').tag_name, 'studio-v0.2.0-rc.1', 'a preview when that is all there is');
  const ext = { tag_name: 'extensions-v0.1.0', draft: false, prerelease: false, assets: [] };
  const alpha = { ...language, tag_name: 'v1.12.0-alpha.1', prerelease: true };
  assert.equal(findRelease([ext, alpha, studio('studio-v0.1.0'), language], 'cli').tag_name, 'v1.11.1');
});

test('each system gets its studio installer, the alternatives beside it', () => {
  const r = studio('studio-v0.1.0');
  const names = p => { const f = studioFiles(r, p); return [f.primary && f.primary.name, f.alts.map(a => a.name)]; };
  assert.deepEqual(names('mac-arm64'), ['mhl.studio_0.1.0_aarch64.dmg', []]);
  assert.deepEqual(names('mac-x64'), ['mhl.studio_0.1.0_x64.dmg', []]);
  assert.deepEqual(names('win-x64'), ['mhl.studio_0.1.0_x64-setup.exe', ['mhl.studio_0.1.0_x64_en-US.msi']]);
  assert.deepEqual(names('linux-x64'), ['mhl.studio_0.1.0_amd64.AppImage', ['mhl.studio_0.1.0_amd64.deb', 'mhl.studio-0.1.0-1.x86_64.rpm']]);
  assert.equal(studioFiles(r, 'win-arm64').emulated, true);
  assert.equal(studioFiles(r, 'linux-arm64').primary, null);
  assert.equal(studioFiles(r, null).primary, null);
});

test('each system gets its CLI archive; Intel Macs have none', () => {
  assert.equal(cliFiles(language, 'mac-arm64').archive.name, 'mhl-1.11.1-darwin-arm64.tar.gz');
  assert.equal(cliFiles(language, 'win-arm64').archive.name, 'mhl-1.11.1-windows-arm64.zip');
  assert.equal(cliFiles(language, 'linux-x64').archive.name, 'mhl-1.11.1-linux-amd64.tar.gz');
  assert.equal(cliFiles(language, 'mac-x64').archive, null);
  assert.equal(cliFiles(language, 'linux-x64').vsix.name, 'mhl-language-0.7.2.vsix');
  assert.equal(cliFiles(language, 'linux-x64').checksums.name, 'checksums.txt');
});
