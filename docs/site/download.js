/* Download page: picks the right installer for the visitor's system from the latest GitHub
   releases. Progressive enhancement — without JavaScript, or when the GitHub API can't be
   reached, every button still leads to the matching releases page. The pure helpers are
   exported for download.test.cjs. */
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else api.init(root);
})(typeof window !== 'undefined' ? window : globalThis, function () {
  'use strict';

  const REPO = 'mh-language/mhl-core-runtime';
  const RELEASES_API = 'https://api.github.com/repos/' + REPO + '/releases?per_page=100';
  const STUDIO_RELEASES = 'https://github.com/' + REPO + '/releases?q=studio-v&expanded=true';
  const LATEST_RELEASE = 'https://github.com/' + REPO + '/releases/latest';
  const PLATFORMS = ['mac-arm64', 'mac-x64', 'win-x64', 'win-arm64', 'linux-x64', 'linux-arm64'];
  const CACHE_KEY = 'mhl-download-releases';
  const CACHE_MS = 15 * 60 * 1000;
  const CHOICE_KEY = 'mhl-download-os';

  /** The platform id for a browser's user agent, or null (phones, tablets, unknown). */
  function platformFrom(info) {
    const ua = String(info.userAgent || '').toLowerCase();
    const platform = String(info.platform || '').toLowerCase();
    const arch = String(info.arch || '').toLowerCase();
    const renderer = String(info.renderer || '');
    if (/iphone|ipad|ipod|android/.test(ua) || info.mobile) return null;
    const arm = arch === 'arm' || arch === 'arm64';
    if (/win/.test(platform) || /windows/.test(ua)) return arm || /arm64|aarch64/.test(ua) ? 'win-arm64' : 'win-x64';
    if (/mac/.test(platform) || /mac os x|macintosh/.test(ua)) {
      if (arch) return arm ? 'mac-arm64' : 'mac-x64';
      // Safari and Firefox report "Intel Mac OS X" on every Mac; the GPU tells them apart.
      if (/intel|amd|radeon|nvidia/i.test(renderer)) return 'mac-x64';
      return 'mac-arm64';
    }
    if (/linux|x11/.test(platform) || /linux|x11/.test(ua)) return arm || /aarch64|arm64|armv8/.test(ua) ? 'linux-arm64' : 'linux-x64';
    return null;
  }

  /** The newest non-draft release of a product: 'studio' (studio-v tags) or 'cli' (v tags). Stable first. */
  function findRelease(releases, product) {
    const ours = (releases || []).filter(r => !r.draft && (product === 'studio'
      ? /^studio-v\d/.test(r.tag_name)
      : /^v\d/.test(r.tag_name)));
    return ours.find(r => !r.prerelease) || (product === 'studio' ? ours[0] : null) || null;
  }

  const STUDIO_FILES = {
    mac: [{ re: /\.dmg$/i, label: '.dmg' }],
    win: [{ re: /-setup\.exe$/i, label: 'Installer (.exe)', pt: 'Instalador (.exe)' },
      { re: /\.msi$/i, label: 'MSI package', pt: 'Pacote MSI' }],
    linux: [{ re: /\.AppImage$/i, label: 'AppImage' },
      { re: /\.deb$/i, label: '.deb — Debian, Ubuntu' },
      { re: /\.rpm$/i, label: '.rpm — Fedora, RHEL' }],
  };
  // tauri-action names ARM builds aarch64 (.dmg, .AppImage, .rpm) or arm64 (.deb, -setup.exe).
  const ARM = /(aarch64|arm64)/i;

  const safe = url => typeof url === 'string' && url.startsWith('https://github.com/');
  const fileOf = (asset, entry) => ({ name: asset.name, url: asset.browser_download_url, size: asset.size, label: entry.label, pt: entry.pt || entry.label });

  /**
   * The studio installers for a platform: `primary` (the one-click download, or null) and
   * `alts` (other formats for the same system), always of the platform's architecture.
   * Windows on ARM falls back to the x64 installer (`emulated`) when a release has no ARM
   * build; Linux on ARM has no such fallback.
   */
  function studioFiles(release, platform) {
    const out = { primary: null, alts: [], emulated: false };
    if (!release || !platform) return out;
    const assets = (release.assets || []).filter(a => safe(a.browser_download_url) && !/\.sig$|\.tar\.gz$|\.json$/i.test(a.name));
    const family = platform.split('-')[0];
    const pick = arm => {
      const found = [];
      for (const entry of STUDIO_FILES[family] || []) {
        const asset = assets.find(a => entry.re.test(a.name) && ARM.test(a.name) === arm);
        if (asset) found.push(fileOf(asset, entry));
      }
      return found;
    };
    let found = pick(platform.endsWith('arm64'));
    if (!found.length && platform === 'win-arm64') {
      found = pick(false);
      out.emulated = found.length > 0;
    }
    out.primary = found[0] || null;
    out.alts = found.slice(1);
    return out;
  }

  const CLI_TARGET = { 'mac-arm64': 'darwin-arm64', 'win-x64': 'windows-amd64', 'win-arm64': 'windows-arm64', 'linux-x64': 'linux-amd64', 'linux-arm64': 'linux-arm64' };

  /** The CLI archive, checksums and VS Code .vsix of a language release, for a platform. */
  function cliFiles(release, platform) {
    const assets = (release && release.assets || []).filter(a => safe(a.browser_download_url));
    const target = CLI_TARGET[platform];
    const archive = target ? assets.find(a => new RegExp('-' + target + '\\.(tar\\.gz|zip)$').test(a.name)) : null;
    return {
      archive: archive || null,
      checksums: assets.find(a => a.name === 'checksums.txt') || null,
      vsix: assets.find(a => /\.vsix$/.test(a.name)) || null,
    };
  }

  const LABELS = {
    'mac-arm64': ['macOS (Apple Silicon)', 'macOS (Apple Silicon)'],
    'mac-x64': ['macOS (Intel)', 'macOS (Intel)'],
    'win-x64': ['Windows', 'Windows'],
    'win-arm64': ['Windows on ARM', 'Windows em ARM'],
    'linux-x64': ['Linux', 'Linux'],
    'linux-arm64': ['Linux on ARM', 'Linux em ARM'],
  };

  function init(win) {
    const doc = win.document;
    const $ = id => doc.getElementById(id);
    const read = (store, key) => { try { return win[store].getItem(key); } catch { return null; } };
    const write = (store, key, value) => { try { win[store].setItem(key, value); } catch { /* storage unavailable */ } };

    let lang = read('localStorage', 'mhl-lang') === 'pt' ? 'pt' : 'en';
    const savedTheme = read('localStorage', 'mhl-theme');
    let theme = savedTheme === 'dark' || savedTheme === 'light' ? savedTheme
      : (win.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
    const t = (en, pt) => (lang === 'pt' ? pt : en);
    const state = { platform: null, detected: null, chosen: false, releases: null, error: false };

    // Elements this script fills in are not translated by data-pt.
    for (const id of ['studio-main-label', 'studio-meta', 'cli-archive', 'os-note', 'quick-or']) {
      const el = $(id);
      if (el) { el.removeAttribute('data-pt'); delete el.dataset.en; }
    }

    function applyLanguage() {
      doc.documentElement.lang = lang === 'pt' ? 'pt-BR' : 'en-US';
      doc.querySelectorAll('[data-pt]').forEach(el => {
        if (el.dataset.en === undefined) el.dataset.en = el.textContent;
        el.textContent = lang === 'pt' ? el.dataset.pt : el.dataset.en;
      });
      doc.querySelectorAll('[data-pt-aria-label]').forEach(el => {
        if (el.dataset.enAriaLabel === undefined) el.dataset.enAriaLabel = el.getAttribute('aria-label');
        el.setAttribute('aria-label', lang === 'pt' ? el.dataset.ptAriaLabel : el.dataset.enAriaLabel);
      });
      doc.querySelectorAll('[data-playground]').forEach(a => { a.href = lang === 'pt' ? 'playground.html' : 'playground-en.html'; });
      const lt = $('lang-toggle');
      lt.textContent = lang === 'pt' ? 'EN' : 'PT';
      lt.setAttribute('aria-label', lang === 'pt' ? 'Switch to English' : 'Mudar para português');
      doc.querySelector('meta[name="description"]').content = t(
        'Download MHL: mhl studio for macOS, Windows and Linux, the mhl CLI in one command, and the VS Code extension.',
        'Baixe o MHL: o mhl studio para macOS, Windows e Linux, a CLI mhl em um comando e a extensão do VS Code.');
      applyTheme();
      render();
    }

    function applyTheme() {
      doc.documentElement.classList.toggle('dark', theme === 'dark');
      const label = theme === 'dark' ? t('Switch to light theme', 'Ativar tema claro') : t('Switch to dark theme', 'Ativar tema escuro');
      const tt = $('theme-toggle');
      tt.setAttribute('aria-label', label);
      tt.title = label;
    }

    const mb = bytes => (bytes / 1048576).toFixed(bytes < 10485760 ? 1 : 0) + ' MB';
    const day = iso => new Date(iso).toLocaleDateString(lang === 'pt' ? 'pt-BR' : 'en-US', { year: 'numeric', month: 'short', day: 'numeric' });
    const link = (href, text) => { const a = doc.createElement('a'); a.href = href; a.textContent = text; a.target = '_blank'; a.rel = 'noreferrer'; return a; };
    const fill = (el, ...parts) => { el.replaceChildren(...parts.map(p => typeof p === 'string' ? doc.createTextNode(p) : p)); };

    function render() {
      const p = state.platform;
      const family = p ? p.split('-')[0] : null;
      const name = p ? LABELS[p][lang === 'pt' ? 1 : 0] : '';

      doc.querySelectorAll('[data-os]').forEach(b => b.setAttribute('aria-checked', String(b.dataset.os === p)));
      $('os-note').textContent = !p
        ? t('Pick your system to see the right files.', 'Escolha o seu sistema para ver os arquivos certos.')
        : state.chosen
          ? t('Showing the files for ' + name + '.', 'Mostrando os arquivos para ' + name + '.')
          : t('Detected ' + name + ' — not right? Pick yours.', 'Detectamos ' + name + ' — não é o seu? Escolha acima.');

      // mhl studio
      const studio = state.releases && findRelease(state.releases, 'studio');
      const main = $('studio-main');
      const meta = $('studio-meta');
      const alt = $('studio-alt');
      $('studio-pending').hidden = !(state.releases && !studio);
      main.hidden = !!(state.releases && !studio);
      alt.hidden = true;
      main.href = STUDIO_RELEASES;
      $('studio-main-label').textContent = t('Download mhl studio', 'Baixar mhl studio');
      if (!state.releases) {
        fill(meta, state.error
          ? t('Couldn\'t reach GitHub — the button opens the studio releases.', 'Não foi possível consultar o GitHub — o botão abre as releases do studio.')
          : t('Looking up the latest release…', 'Buscando a última versão…'));
      } else if (!studio) {
        fill(meta, '');
      } else {
        const files = studioFiles(studio, p);
        const version = studio.tag_name.replace(/^studio-v/, '');
        const notes = link(studio.html_url, t('release notes ↗', 'notas da versão ↗'));
        if (files.primary) {
          main.href = files.primary.url;
          $('studio-main-label').textContent = t('Download for ', 'Baixar para ') + name;
          fill(meta, 'mhl studio ' + version + ' · ' + t(files.primary.label, files.primary.pt) + ' · ' + mb(files.primary.size) + ' · ' + day(studio.published_at) + ' · ', notes);
          if (files.emulated) meta.append(doc.createTextNode(' · ' + t('x64 build, runs under emulation', 'versão x64, roda sob emulação')));
          if (files.alts.length) {
            alt.replaceChildren(...files.alts.map(f => {
              const li = doc.createElement('li');
              const a = doc.createElement('a');
              a.href = f.url;
              a.append(doc.createTextNode(t(f.label, f.pt)));
              const size = doc.createElement('span');
              size.textContent = mb(f.size);
              a.append(size);
              li.append(a);
              return li;
            }));
            alt.hidden = false;
          }
        } else {
          main.href = studio.html_url;
          $('studio-main-label').textContent = t('See all installers', 'Ver todos os instaladores');
          fill(meta, 'mhl studio ' + version + ' · ' + (p === 'linux-arm64'
            ? t('this release has no Linux ARM build', 'esta versão não tem build para Linux ARM')
            : t('pick your system above', 'escolha seu sistema acima')) + ' · ', notes);
        }
      }
      doc.querySelectorAll('[data-os-only]').forEach(el => { el.hidden = !!family && el.dataset.osOnly !== family; });

      // Quick install card: the same studio download, and the CLI command for this system.
      const quick = $('quick-studio');
      const quickMeta = $('quick-studio-meta');
      const studioReady = studio && studioFiles(studio, p).primary;
      quick.hidden = !!(state.releases && !studio);
      quick.href = studioReady ? studioReady.url : '#studio';
      $('quick-studio-label').textContent = studioReady ? t('Download mhl studio for ', 'Baixar mhl studio para ') + name : t('Download mhl studio', 'Baixar mhl studio');
      if (studioReady) fill(quickMeta, 'mhl studio ' + studio.tag_name.replace(/^studio-v/, '') + ' · ' + t(studioReady.label, studioReady.pt) + ' · ' + mb(studioReady.size));
      else if (state.releases && !studio) {
        const build = doc.createElement('a');
        build.href = '#studio';
        build.textContent = t('build it from source', 'compile a partir do código');
        fill(quickMeta, t('mhl studio: first release coming soon — or ', 'mhl studio: primeira versão em breve — ou '), build, '.');
      } else fill(quickMeta, '');
      const noCli = p === 'mac-x64';
      $('quick-or').textContent = state.releases && !studio
        ? t('Install the mhl CLI from the terminal:', 'Instale a CLI mhl pelo terminal:')
        : t('or install the CLI from the terminal:', 'ou instale a CLI pelo terminal:');
      $('quick-cli-box').hidden = noCli;
      $('quick-or').hidden = noCli;
      $('quick-cli').textContent = family === 'win'
        ? 'irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.ps1 | iex'
        : 'curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.sh | sh';
      $('quick-cli-meta').textContent = noCli
        ? t('The mhl CLI has no Intel Mac build; mhl studio bundles the runtime.', 'A CLI mhl não tem versão para Mac Intel; o mhl studio já traz o runtime.')
        : t('Installs mhl — and the VS Code extension, if VS Code is installed.', 'Instala o mhl — e a extensão do VS Code, se o VS Code estiver instalado.');

      // mhl CLI
      $('cli-unix').open = family !== 'win';
      $('cli-windows').open = family === 'win';
      $('cli-intel').hidden = p !== 'mac-x64';
      const cliRelease = state.releases && findRelease(state.releases, 'cli');
      const archive = $('cli-archive');
      const checksums = $('cli-checksums');
      const cliMeta = $('cli-meta');
      archive.href = LATEST_RELEASE;
      archive.textContent = t('Latest release page ↗', 'Página da última versão ↗');
      checksums.hidden = true;
      fill(cliMeta, '');
      if (cliRelease) {
        const files = cliFiles(cliRelease, p);
        fill(cliMeta, t('Latest: ', 'Última versão: ') + cliRelease.tag_name + ' · ' + day(cliRelease.published_at) + ' · ', link(cliRelease.html_url, t('release notes ↗', 'notas da versão ↗')));
        if (files.archive) {
          archive.href = files.archive.browser_download_url;
          archive.textContent = files.archive.name + ' (' + mb(files.archive.size) + ')';
        }
        if (files.checksums && files.archive) {
          checksums.href = files.checksums.browser_download_url;
          checksums.hidden = false;
        }
        if (files.vsix) {
          $('vsix-link').href = files.vsix.browser_download_url;
          $('vsix-link').textContent = files.vsix.name;
        }
      }
      $('cli-archive-row').hidden = p === 'mac-x64';
    }

    function choose(platform, chosen) {
      state.platform = platform;
      state.chosen = chosen;
      if (chosen) write('localStorage', CHOICE_KEY, platform);
      render();
    }

    async function detect() {
      const nav = win.navigator;
      const info = { userAgent: nav.userAgent, platform: (nav.userAgentData && nav.userAgentData.platform) || nav.platform, mobile: nav.userAgentData && nav.userAgentData.mobile };
      try {
        if (nav.userAgentData && nav.userAgentData.getHighEntropyValues) {
          const hi = await nav.userAgentData.getHighEntropyValues(['architecture', 'bitness']);
          info.arch = hi.architecture === 'arm' ? 'arm' : hi.architecture ? 'x86' : '';
        }
      } catch { /* not available */ }
      if (!info.arch) {
        try {
          const gl = doc.createElement('canvas').getContext('webgl');
          const ext = gl && gl.getExtension('WEBGL_debug_renderer_info');
          info.renderer = ext ? gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) : '';
        } catch { /* no WebGL */ }
      }
      return platformFrom(info);
    }

    async function loadReleases() {
      try {
        const cached = JSON.parse(read('sessionStorage', CACHE_KEY) || 'null');
        if (cached && Date.now() - cached.at < CACHE_MS && Array.isArray(cached.data)) return cached.data;
      } catch { /* ignore a corrupt cache */ }
      const res = await win.fetch(RELEASES_API, { headers: { Accept: 'application/vnd.github+json' } });
      if (!res.ok) throw new Error('GitHub API ' + res.status);
      const data = (await res.json()).map(r => ({
        tag_name: r.tag_name, draft: r.draft, prerelease: r.prerelease, published_at: r.published_at, html_url: r.html_url,
        assets: (r.assets || []).map(a => ({ name: a.name, size: a.size, browser_download_url: a.browser_download_url })),
      }));
      write('sessionStorage', CACHE_KEY, JSON.stringify({ at: Date.now(), data }));
      return data;
    }

    // Controls
    $('lang-toggle').addEventListener('click', () => { lang = lang === 'pt' ? 'en' : 'pt'; write('localStorage', 'mhl-lang', lang); applyLanguage(); });
    $('theme-toggle').addEventListener('click', () => { theme = theme === 'dark' ? 'light' : 'dark'; write('localStorage', 'mhl-theme', theme); applyTheme(); });
    const radios = [...doc.querySelectorAll('[data-os]')];
    radios.forEach((b, i) => {
      b.addEventListener('click', () => choose(b.dataset.os, true));
      b.addEventListener('keydown', e => {
        const step = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 0;
        if (!step) return;
        e.preventDefault();
        const next = radios[(i + step + radios.length) % radios.length];
        choose(next.dataset.os, true);
        next.focus();
      });
    });
    const status = $('copy-status');
    doc.querySelectorAll('[data-copy]').forEach(button => {
      button.addEventListener('click', async () => {
        const code = $(button.dataset.copy);
        try {
          await win.navigator.clipboard.writeText(code.textContent);
          button.textContent = t('Copied!', 'Copiado!');
          status.textContent = t('Command copied.', 'Comando copiado.');
        } catch {
          const range = doc.createRange();
          range.selectNodeContents(code);
          const selection = win.getSelection();
          selection.removeAllRanges();
          selection.addRange(range);
          button.textContent = t('Selected', 'Selecionado');
        }
        setTimeout(() => { button.textContent = t('Copy', 'Copiar'); }, 1800);
      });
    });

    const saved = read('localStorage', CHOICE_KEY);
    if (PLATFORMS.includes(saved)) { state.platform = saved; state.chosen = true; }
    applyLanguage();
    if (!state.chosen) detect().then(p => { if (!state.chosen) { state.detected = p; state.platform = p; render(); } });
    loadReleases()
      .then(data => { state.releases = data; render(); })
      .catch(() => { state.error = true; render(); });
  }

  return { platformFrom, findRelease, studioFiles, cliFiles, init, PLATFORMS };
});
