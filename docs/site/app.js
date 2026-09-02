(() => {
  const root = document.documentElement;
  const themeButton = document.querySelector('#theme-toggle');
  const savedTheme = localStorage.getItem('mhl-theme');
  const prefersDark = matchMedia('(prefers-color-scheme: dark)').matches;

  function setTheme(theme) {
    root.classList.toggle('dark', theme === 'dark');
    if (themeButton) {
      themeButton.textContent = theme === 'dark' ? '☾' : '☼';
      themeButton.setAttribute('aria-label', theme === 'dark' ? 'Use light theme' : 'Use dark theme');
    }
  }

  setTheme(savedTheme || (prefersDark ? 'dark' : 'light'));
  themeButton?.addEventListener('click', () => {
    const next = root.classList.contains('dark') ? 'light' : 'dark';
    setTheme(next);
    localStorage.setItem('mhl-theme', next);
  });

  const escapeHtml = (value) => value.replace(/[&<>"']/g, (char) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[char]));

  const keywords = new Set(
    'import from as export loop var mem if else while try catch finally return break goto for in self fail pause'.split(' ')
  );
  const declarations = new Set(
    'agent memory tool pipeline prompt extension parallel step input output test describe'.split(' ')
  );
  const constants = new Set(['true', 'false', 'null']);

  function span(className, value) {
    return `<span class="${className}">${value}</span>`;
  }

  function stringBody(value) {
    let result = '';
    let cursor = 0;
    const interpolation = /\\?\$\{[^}]*\}/g;
    for (const match of value.matchAll(interpolation)) {
      result += escapeHtml(value.slice(cursor, match.index));
      const className = match[0].startsWith('\\') ? 'tok-escape' : 'tok-var';
      result += span(className, escapeHtml(match[0]));
      cursor = match.index + match[0].length;
    }
    return result + escapeHtml(value.slice(cursor));
  }

  function highlight(source) {
    let html = '';
    let i = 0;
    while (i < source.length) {
      if (source.startsWith('//', i)) {
        const end = source.indexOf('\n', i);
        const value = source.slice(i, end < 0 ? source.length : end);
        html += span('tok-comment', escapeHtml(value));
        i += value.length;
        continue;
      }
      if (source.startsWith('"""', i)) {
        const end = source.indexOf('"""', i + 3);
        const finish = end < 0 ? source.length : end + 3;
        const value = source.slice(i, finish);
        html += span('tok-string', stringBody(value));
        i = finish;
        continue;
      }
      if (source[i] === '"') {
        let end = i + 1;
        while (end < source.length) {
          if (source[end] === '\\') end += 2;
          else if (source[end++] === '"') break;
        }
        const value = source.slice(i, end);
        html += span('tok-string', stringBody(value));
        i = end;
        continue;
      }
      const duration = source.slice(i).match(/^\d+(?:\.\d+)?(?:ms|s|m|h|d)\b/);
      if (duration) {
        html += span('tok-number', duration[0]);
        i += duration[0].length;
        continue;
      }
      const number = source.slice(i).match(/^\d+(?:\.\d+)?\b/);
      if (number) {
        html += span('tok-number', number[0]);
        i += number[0].length;
        continue;
      }
      const identifier = source.slice(i).match(/^[a-zA-Z_][a-zA-Z0-9_]*/);
      if (identifier) {
        const value = identifier[0];
        const after = source.slice(i + value.length);
        let className = 'tok-variable';
        if (declarations.has(value)) className = 'tok-declaration';
        else if (keywords.has(value)) className = 'tok-keyword';
        else if (constants.has(value)) className = 'tok-boolean';
        else if (/^\s*:/.test(after)) className = 'tok-property';
        else if (/^\s*\(/.test(after)) className = 'tok-function';
        html += span(className, value);
        i += value.length;
        continue;
      }
      const operator = source.slice(i).match(/^(?:==|!=|<=|>=|&&|\|\||->|\.\.|[+*/%<>=!^-])/);
      if (operator) {
        html += span('tok-operator', escapeHtml(operator[0]));
        i += operator[0].length;
        continue;
      }
      const value = source[i];
      html += /[{}[\]().,:]/.test(value)
        ? span('tok-punctuation', escapeHtml(value))
        : escapeHtml(value);
      i += 1;
    }
    return html;
  }

  function highlightJson(source) {
    let html = '';
    let i = 0;
    while (i < source.length) {
      if (source[i] === '"') {
        let end = i + 1;
        while (end < source.length) {
          if (source[end] === '\\') end += 2;
          else if (source[end++] === '"') break;
        }
        const value = source.slice(i, end);
        const className = /^\s*:/.test(source.slice(end)) ? 'tok-json-key' : 'tok-json-string';
        html += span(className, escapeHtml(value));
        i = end;
        continue;
      }
      const number = source.slice(i).match(/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/);
      if (number) {
        html += span('tok-json-number', number[0]);
        i += number[0].length;
        continue;
      }
      const literal = source.slice(i).match(/^(?:true|false|null)\b/);
      if (literal) {
        html += span('tok-json-literal', literal[0]);
        i += literal[0].length;
        continue;
      }
      const value = source[i];
      html += '{}[],:'.includes(value)
        ? span('tok-json-punctuation', escapeHtml(value))
        : escapeHtml(value);
      i += 1;
    }
    return html;
  }

  document.querySelectorAll('code.language-mhl').forEach((block) => {
    block.innerHTML = highlight(block.textContent);
  });
  document.querySelectorAll('code.language-json').forEach((block) => {
    block.innerHTML = highlightJson(block.textContent);
  });

  document.querySelectorAll('[data-copy="copy"]').forEach((button) => {
    const idleText = button.textContent;
    button.addEventListener('click', async () => {
      const code = button.parentElement.querySelector('code')?.textContent || '';
      try {
        await navigator.clipboard.writeText(code);
        button.textContent = 'Copied';
      } catch {
        button.textContent = 'Select the code';
      }
      setTimeout(() => { button.textContent = idleText; }, 1400);
    });
  });

  const links = [...document.querySelectorAll('.nav-link')];
  const sections = links
    .map((link) => document.querySelector(link.getAttribute('href')))
    .filter(Boolean);
  const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {
      if (!entry.isIntersecting) return;
      links.forEach((link) => {
        link.classList.toggle('active', link.getAttribute('href') === `#${entry.target.id}`);
      });
    });
  }, { rootMargin: '-20% 0px -65% 0px' });
  sections.forEach((section) => observer.observe(section));

  const search = document.querySelector('#site-search');
  const searchable = [...document.querySelectorAll('.doc-section')];
  search?.addEventListener('input', () => {
    const query = search.value.trim().toLocaleLowerCase('en-US');
    searchable.forEach((section) => {
      const haystack = `${section.dataset.title || ''} ${section.textContent}`.toLocaleLowerCase('en-US');
      section.classList.toggle('is-hidden', Boolean(query) && !haystack.includes(query));
    });
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === '/' && document.activeElement !== search && !event.metaKey && !event.ctrlKey) {
      event.preventDefault();
      search?.focus();
    }
  });
})();
