(() => {
  const root = document.documentElement;
  const themeButton = document.querySelector('#theme-toggle');
  const savedTheme = localStorage.getItem('mhl-theme');
  if (savedTheme === 'dark' || (!savedTheme && matchMedia('(prefers-color-scheme: dark)').matches)) root.classList.add('dark');

  themeButton.addEventListener('click', () => {
    root.classList.toggle('dark');
    localStorage.setItem('mhl-theme', root.classList.contains('dark') ? 'dark' : 'light');
  });

  // i18n: the page ships with English as its static content (so it reads
  // correctly even with JS disabled or before this script runs) and carries
  // the Portuguese translation in each element's `data-pt` attribute. On
  // first pass we snapshot the shipped English into `data-en` so a later
  // switch back to English can restore it, then swap `innerHTML` (or, for
  // elements marked `data-i18n-attr`, one specific attribute — used for
  // things translation can't reach via text content, like <title>'s meta
  // description or an input's placeholder) between the two.
  const langButton = document.querySelector('#lang-toggle');
  let currentLang = localStorage.getItem('mhl-lang') === 'pt' ? 'pt' : 'en';
  const i18nEls = [...document.querySelectorAll('[data-pt]')];
  i18nEls.forEach((el) => {
    const attr = el.getAttribute('data-i18n-attr');
    el.dataset.en = attr ? (el.getAttribute(attr) || '') : el.innerHTML;
  });
  const copyMessages = {
    en: { copied: 'Copied', selectCode: 'Select the code' },
    pt: { copied: 'Copiado', selectCode: 'Selecione o código' },
  };

  function applyLang(lang) {
    currentLang = lang;
    root.lang = lang === 'pt' ? 'pt-BR' : 'en';
    i18nEls.forEach((el) => {
      const attr = el.getAttribute('data-i18n-attr');
      const value = lang === 'pt' ? el.dataset.pt : el.dataset.en;
      if (attr) el.setAttribute(attr, value);
      else el.innerHTML = value;
    });
    if (langButton) {
      langButton.textContent = lang === 'pt' ? 'EN' : 'PT-BR';
      langButton.setAttribute('aria-label', lang === 'pt' ? 'Switch to English' : 'Switch to Portuguese');
    }
    localStorage.setItem('mhl-lang', lang);
  }

  applyLang(currentLang);
  if (langButton) langButton.addEventListener('click', () => applyLang(currentLang === 'pt' ? 'en' : 'pt'));

  const escapeHtml = (value) => value.replace(/[&<>"']/g, (char) => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' }[char]));
  const keywords = new Set('import from use as export mcp_server agent skill memory tool loop pipeline prompt step input test describe var if else while try catch finally return break goto for in self fail'.split(' '));
  const declarations = new Set('agent skill memory tool pipeline prompt mcp_server step input'.split(' '));
  const booleans = new Set(['true', 'false', 'null']);

  function span(className, value) { return `<span class="${className}">${value}</span>`; }
  function stringBody(value) {
    let result = '', cursor = 0;
    const interpolation = /\\?\$\{[^}]*\}/g;
    for (const match of value.matchAll(interpolation)) {
      result += escapeHtml(value.slice(cursor, match.index));
      const literal = match[0].startsWith('\\');
      result += literal ? span('tok-escape', escapeHtml(match[0])) : span('tok-var', escapeHtml(match[0]));
      cursor = match.index + match[0].length;
    }
    return result + escapeHtml(value.slice(cursor));
  }

  function highlight(source) {
    let html = '', i = 0;
    while (i < source.length) {
      if (source.startsWith('//', i)) {
        const end = source.indexOf('\n', i);
        const value = source.slice(i, end < 0 ? source.length : end);
        html += span('tok-comment', escapeHtml(value)); i += value.length; continue;
      }
      if (source.startsWith('"""', i)) {
        const end = source.indexOf('"""', i + 3);
        const finish = end < 0 ? source.length : end + 3;
        const value = source.slice(i, finish);
        html += span('tok-string', stringBody(value)); i = finish; continue;
      }
      if (source[i] === '"') {
        let end = i + 1;
        while (end < source.length) { if (source[end] === '\\') end += 2; else if (source[end++] === '"') break; }
        const value = source.slice(i, end);
        html += span('tok-string', stringBody(value)); i = end; continue;
      }
      const duration = source.slice(i).match(/^\d+(?:\.\d+)?(?:ms|s|m|h|d)\b/);
      if (duration) { html += span('tok-number', escapeHtml(duration[0])); i += duration[0].length; continue; }
      const number = source.slice(i).match(/^\d+(?:\.\d+)?\b/);
      if (number) { html += span('tok-number', number[0]); i += number[0].length; continue; }
      const identifier = source.slice(i).match(/^[a-zA-Z_][a-zA-Z0-9_]*/);
      if (identifier) {
        const value = identifier[0], after = source.slice(i + value.length);
        let className = 'tok-variable';
        if (declarations.has(value)) className = 'tok-declaration';
        else if (keywords.has(value)) className = 'tok-keyword';
        else if (booleans.has(value)) className = 'tok-boolean';
        else if (/^\s*:/.test(after)) className = 'tok-property';
        else if (/^\s*\(/.test(after)) className = 'tok-function';
        html += span(className, value); i += value.length; continue;
      }
      const operator = source.slice(i).match(/^(?:==|!=|<=|>=|&&|\|\||->|\.\.|[+*/%<>=!-])/);
      if (operator) { html += span('tok-operator', escapeHtml(operator[0])); i += operator[0].length; continue; }
      const punctuation = source[i];
      html += /[{}[\]().,:]/.test(punctuation) ? span('tok-punctuation', escapeHtml(punctuation)) : escapeHtml(punctuation);
      i += 1;
    }
    return html;
  }

  document.querySelectorAll('code.language-mhl').forEach((block) => {
    const source = block.textContent;
    block.innerHTML = highlight(source);
  });

  document.querySelectorAll('[data-copy="copy"]').forEach((button) => {
    button.addEventListener('click', async () => {
      const code = button.parentElement.querySelector('code').textContent;
      const idle = () => { button.textContent = currentLang === 'pt' ? button.dataset.pt : button.dataset.en; };
      try { await navigator.clipboard.writeText(code); button.textContent = copyMessages[currentLang].copied; setTimeout(idle, 1400); }
      catch { button.textContent = copyMessages[currentLang].selectCode; setTimeout(idle, 1400); }
    });
  });

  const links = [...document.querySelectorAll('.nav-link')];
  const sections = links.map((link) => document.querySelector(link.getAttribute('href'))).filter(Boolean);
  const observer = new IntersectionObserver((entries) => entries.forEach((entry) => { if (entry.isIntersecting) { links.forEach((link) => link.classList.toggle('active', link.getAttribute('href') === `#${entry.target.id}`)); } }), { rootMargin:'-20% 0px -65% 0px' });
  sections.forEach((section) => observer.observe(section));

  const search = document.querySelector('#site-search');
  const searchable = [...document.querySelectorAll('.doc-section')];
  search.addEventListener('input', () => {
    const query = search.value.trim().toLowerCase();
    searchable.forEach((section) => section.classList.toggle('is-hidden', query && !(section.dataset.title + ' ' + section.textContent).toLowerCase().includes(query)));
  });
  document.addEventListener('keydown', (event) => { if (event.key === '/' && document.activeElement !== search && !event.metaKey && !event.ctrlKey) { event.preventDefault(); search.focus(); } });
})();
