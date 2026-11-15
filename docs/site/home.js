/* Progressive enhancement for the static homepage; no browser-side MHL execution. */
(() => {
  'use strict';
  const read = key => {
    try { return localStorage.getItem(key); } catch { return null; }
  };
  const write = (key, value) => {
    try { localStorage.setItem(key, value); } catch { /* Keep controls usable without storage. */ }
  };
  let lang = read('mhl-lang') === 'pt' ? 'pt' : 'en';
  const savedTheme = read('mhl-theme');
  let theme = savedTheme === 'dark' || savedTheme === 'light' ? savedTheme
    : (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  const languageButton = document.querySelector('#lang-toggle');
  const themeButton = document.querySelector('#theme-toggle');
  const status = document.querySelector('#copy-status');
  const copyTimers = new WeakMap();

  function themeLabel() {
    const label = lang === 'pt'
      ? (theme === 'dark' ? 'Ativar tema claro' : 'Ativar tema escuro')
      : (theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme');
    themeButton.setAttribute('aria-label', label);
    themeButton.title = label;
  }

  function applyLanguage() {
    document.documentElement.lang = lang === 'pt' ? 'pt-BR' : 'en-US';
    document.querySelectorAll('[data-pt]').forEach(element => {
      if (element.dataset.en === undefined) element.dataset.en = element.textContent;
      element.textContent = lang === 'pt' ? element.dataset.pt : element.dataset.en;
    });
    document.querySelectorAll('[data-pt-aria-label]').forEach(element => {
      if (element.dataset.enAriaLabel === undefined) element.dataset.enAriaLabel = element.getAttribute('aria-label');
      element.setAttribute('aria-label', lang === 'pt' ? element.dataset.ptAriaLabel : element.dataset.enAriaLabel);
    });
    document.querySelectorAll('[data-playground]').forEach(link => {
      const fragment = link.hash;
      link.href = (lang === 'pt' ? 'playground.html' : 'playground-en.html') + fragment;
    });
    languageButton.textContent = lang === 'pt' ? 'EN' : 'PT';
    languageButton.setAttribute('aria-label', lang === 'pt' ? 'Switch to English' : 'Mudar para português');
    document.querySelector('meta[name="description"]').content = lang === 'pt'
      ? 'Uma linguagem para workflows de IA. Declare agentes, prompts e etapas em um arquivo .mh e execute com a CLI do MHL.'
      : 'A language for AI workflows. Define agents, prompts and execution steps in a readable .mh file, and run it with the MHL CLI.';
    status.textContent = '';
    themeLabel();
  }

  function applyTheme() {
    document.documentElement.classList.toggle('dark', theme === 'dark');
    themeLabel();
  }

  languageButton.addEventListener('click', () => {
    lang = lang === 'pt' ? 'en' : 'pt';
    write('mhl-lang', lang);
    applyLanguage();
  });
  themeButton.addEventListener('click', () => {
    theme = theme === 'dark' ? 'light' : 'dark';
    write('mhl-theme', theme);
    applyTheme();
  });

  document.querySelectorAll('code.language-mhl').forEach(code => {
    code.dataset.raw = code.textContent;
    if (window.MhlPlayground) code.innerHTML = window.MhlPlayground.highlight(code.dataset.raw);
  });

  const tabs = [...document.querySelectorAll('[role="tab"]')];
  function selectTab(tab, focus = false) {
    tabs.forEach(item => {
      const selected = item === tab;
      item.setAttribute('aria-selected', String(selected));
      item.tabIndex = selected ? 0 : -1;
      document.getElementById(item.getAttribute('aria-controls')).hidden = !selected;
    });
    if (focus) tab.focus();
  }
  tabs.forEach((tab, index) => {
    tab.addEventListener('click', () => selectTab(tab));
    tab.addEventListener('keydown', event => {
      let next;
      if (event.key === 'ArrowRight') next = (index + 1) % tabs.length;
      if (event.key === 'ArrowLeft') next = (index + tabs.length - 1) % tabs.length;
      if (event.key === 'Home') next = 0;
      if (event.key === 'End') next = tabs.length - 1;
      if (next !== undefined) {
        event.preventDefault();
        selectTab(tabs[next], true);
      }
    });
  });
  document.querySelectorAll('[data-copy]').forEach(button => {
    button.addEventListener('click', async () => {
      const code = document.getElementById(button.dataset.copy);
      const text = code.dataset.raw ?? code.textContent;
      clearTimeout(copyTimers.get(button));
      try {
        await navigator.clipboard.writeText(text);
        button.textContent = lang === 'pt' ? 'Copiado!' : 'Copied!';
        status.textContent = lang === 'pt' ? 'Código copiado.' : 'Code copied.';
      } catch {
        const range = document.createRange();
        range.selectNodeContents(code);
        const selection = window.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
        button.textContent = lang === 'pt' ? 'Selecionado' : 'Selected';
        status.textContent = lang === 'pt' ? 'Use o comando de copiar para copiar o texto selecionado.' : 'Use your copy command to copy the selected text.';
      }
      copyTimers.set(button, setTimeout(() => { button.textContent = lang === 'pt' ? 'Copiar' : 'Copy'; }, 1800));
    });
  });

  applyTheme();
  applyLanguage();
})();
