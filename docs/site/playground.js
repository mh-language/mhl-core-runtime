(function () {
  "use strict";
  const english = document.documentElement.lang === "en";
  const locale = english ? "en" : "pt-BR";
  const t = (pt, en) => english ? en : pt;
  const storageKey = english ? "mhl-playground-v2-en" : "mhl-playground-v2";
  const lessons = window.MhlLessons;
  const core = window.MhlPlayground;
  const $ = selector => document.querySelector(selector);
  const editor = $("#editor");
  let storage;
  try { storage = window.localStorage; } catch { /* Editing remains available. */ }
  const state = core.readState(storage, lessons, storageKey);
  let current = lessons.findIndex(lesson => lesson.id === state.current);
  let saveTimer;
  let tabInserts = false;
  let undoEntries = [];
  let historyIndex = 0;
  let restoring = false;
  let previousValue = "";

  function save() {
    clearTimeout(saveTimer);
    try {
      storage.setItem(storageKey, JSON.stringify(state));
      $("#save-status").textContent = t("Rascunho salvo neste navegador", "Draft saved in this browser");
    } catch {
      $("#save-status").textContent = t("Rascunho só nesta aba; baixe para guardar", "Draft kept in this tab only; download to save");
    }
  }
  function changed() { return editor.value !== lessons[current].code; }
  function syncEditor() {
    $("#highlight code").innerHTML = core.highlight(editor.value) + "\n";
    const count = editor.value.split("\n").length;
    $("#line-numbers").textContent = Array.from({length: count}, (_, i) => i + 1).join("\n") + "\n";
    $("#edit-badge").textContent = changed() ? t("Editado", "Edited") : "Original";
    $("#reset-button").disabled = !changed();
    const before = editor.value.slice(0, editor.selectionStart).split("\n");
    $("#cursor-position").textContent = "Ln " + before.length + ", Col " + (before.at(-1).length + 1);
    syncScroll();
  }
  function syncScroll() {
    $("#highlight").scrollTop = editor.scrollTop;
    $("#highlight").scrollLeft = editor.scrollLeft;
    $("#line-numbers").scrollTop = editor.scrollTop;
  }
  function remember() {
    if (changed()) state.drafts[lessons[current].id] = editor.value;
    else delete state.drafts[lessons[current].id];
    $("#save-status").textContent = t("Salvando…", "Saving…");
    clearTimeout(saveTimer);
    saveTimer = setTimeout(save, 300);
  }
  function input() {
    if (!restoring && editor.value !== previousValue) {
      undoEntries.splice(historyIndex + 1);
      undoEntries.push({ value: editor.value, position: editor.selectionStart });
      if (undoEntries.length > 100) undoEntries.shift();
      historyIndex = undoEntries.length - 1;
    }
    previousValue = editor.value;
    syncEditor(); remember();
    $("#diagnostic").textContent = t("Alterado; verifique novamente", "Changed; check again");
    $("#diagnostic-list").replaceChildren();
    reference();
  }
  function undo(direction) {
    const next = historyIndex + direction;
    if (next < 0 || next >= undoEntries.length) return;
    historyIndex = next;
    editor.value = undoEntries[next].value;
    editor.setSelectionRange(undoEntries[next].position, undoEntries[next].position);
    restoring = true; input(); restoring = false;
  }
  function reference() {
    $("#output").textContent = lessons[current].result;
    $("#reference-status").textContent = changed()
      ? t("Referência do exemplo original. Suas edições não são executadas aqui.", "Original example reference. Your edits are not executed here.")
      : t("Resultado esperado do exemplo original na CLI; não é uma execução no navegador.", "Expected result of the original example in the CLI; not a browser execution.");
  }
  function analyze() {
    const errors = core.diagnostics(editor.value, locale);
    const list = $("#diagnostic-list");
    list.replaceChildren();
    for (const error of errors) {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = t("Linha ", "Line ") + error.line + t(", coluna ", ", column ") + error.column + ": " + error.message;
      button.addEventListener("click", () => {
        editor.focus();
        editor.setSelectionRange(error.offset, error.offset + 1);
        editor.scrollTop = Math.max(0, (error.line - 3) * parseFloat(getComputedStyle(editor).lineHeight));
        syncEditor();
      });
      list.append(button);
    }
    $("#diagnostic").textContent = errors.length
      ? errors.length + t(" problema(s) de fechamento", " closure issue(s)")
      : t("Strings e delimitadores fechados. Use mhl lint para validar a linguagem.", "Strings and delimiters are closed. Use mhl lint to validate the language.");
  }
  function renderNav() {
    const query = $("#lesson-search").value.toLocaleLowerCase(locale).trim();
    const list = $("#lesson-list");
    list.replaceChildren();
    let lastGroup = "", count = 0;
    for (const [index, lesson] of lessons.entries()) {
      if (query && ![lesson.title, lesson.group, lesson.description, ...lesson.concepts].join(" ").toLocaleLowerCase(locale).includes(query)) continue;
      count++;
      if (lesson.group !== lastGroup) {
        const label = document.createElement("h3");
        label.className = "nav-group"; label.textContent = lesson.group;
        list.append(label); lastGroup = lesson.group;
      }
      const button = document.createElement("button");
      button.className = "lesson-button" + (index === current ? " active" : "");
      button.type = "button";
      if (index === current) button.setAttribute("aria-current", "step");
      const completed = state.completed.includes(lesson.id);
      button.innerHTML = '<span class="lesson-number">' + String(index + 1).padStart(2, "0") +
        '</span><span>' + core.escapeHtml(lesson.title) + '</span><span class="lesson-check" aria-hidden="true">' + (completed ? "✓" : "") + "</span>";
      button.setAttribute("aria-label", lesson.title + (completed ? t(", concluída", ", completed") : ""));
      button.addEventListener("click", () => loadLesson(index, true));
      list.append(button);
    }
    if (!count) list.textContent = t("Nenhuma etapa encontrada.", "No lessons found.");
  }
  function progress() {
    const completed = state.completed.includes(lessons[current].id);
    $("#complete-button").textContent = completed ? t("✓ Etapa concluída", "✓ Lesson completed") : t("Marcar como concluída", "Mark as completed");
    $("#complete-button").setAttribute("aria-pressed", String(completed));
    $("#progress-label").textContent = state.completed.length + t(" de ", " of ") + lessons.length + t(" concluídas", " completed");
    const percent = Math.round(state.completed.length / lessons.length * 100);
    $("#progress-percent").textContent = percent + "%";
    $("#progress-bar").style.width = percent + "%";
    $("#progress-track").setAttribute("aria-valuenow", String(state.completed.length));
    $("#progress-track").setAttribute("aria-valuemax", String(lessons.length));
    $("#step-dots").textContent = t("Etapa ", "Lesson ") + (current + 1) + " / " + lessons.length;
  }
  function loadLesson(index, focus = false) {
    current = index;
    const lesson = lessons[index];
    state.current = lesson.id;
    $("#language-switch").href = (english ? "playground.html" : "playground-en.html") + "#" + lesson.id;
    $("#lesson-kicker").textContent = lesson.group;
    $("#lesson-title").textContent = lesson.title;
    $("#lesson-description").textContent = lesson.description;
    $("#file-name").textContent = lesson.file;
    $("#concepts").innerHTML = lesson.concepts.map(item => '<span class="concept">' + core.escapeHtml(item) + "</span>").join("");
    $("#tip").innerHTML = t("<strong>O que observar</strong><ul>", "<strong>What to notice</strong><ul>") + lesson.notes.map(note => "<li>" + core.escapeHtml(note) + "</li>").join("") + "</ul>";
    $("#challenge").textContent = lesson.challenge;
    $("#requirements").textContent = lesson.requirements;
    $("#source-link").href = "https://github.com/mh-language/mhl-core-runtime/blob/main/" + lesson.source;
    $("#source-link").textContent = t("Exemplo em sample/ ↗", "Example in sample/ ↗");
    $("#cli-command").textContent = "mhl lint " + lesson.file +
      (lesson.mode === "run" ? "\nmhl run " + lesson.file + (lesson.args ? " " + lesson.args : "") + " --dry-run" : "") +
      "\nmhl " + lesson.mode + " " + lesson.file + (lesson.args ? " " + lesson.args : "") +
      (lesson.resume ? "\n" + lesson.resume : "");
    editor.value = state.drafts[lesson.id] ?? lesson.code;
    previousValue = editor.value;
    undoEntries = [{ value: editor.value, position: 0 }];
    historyIndex = 0;
    editor.setSelectionRange(0, 0);
    editor.scrollTop = 0; editor.scrollLeft = 0;
    $("#diagnostic").textContent = t("Verificação local: fechamento de strings e delimitadores", "Local check: string and delimiter closure");
    $("#diagnostic-list").replaceChildren();
    $("#previous-button").disabled = index === 0;
    $("#next-button").disabled = index === lessons.length - 1;
    $("#reference-details").open = !lesson.external;
    syncEditor(); reference(); renderNav(); progress(); save();
    if (location.hash !== "#" + lesson.id) window.history.replaceState(null, "", "#" + lesson.id);
    if (focus) {
      const heading = $("#lesson-title");
      heading.focus({ preventScroll: true });
      const box = heading.getBoundingClientRect();
      if (box.top < 0 || box.bottom > innerHeight) heading.scrollIntoView({block: "start"});
      const active = $("#lesson-list .active");
      // Keep the active lesson visible in the horizontal mobile navigation.
      if (active && getComputedStyle($("#lesson-list")).display === "flex") {
        $("#lesson-list").scrollLeft = active.offsetLeft - $("#lesson-list").offsetLeft;
      }
    }
  }
  async function copy(value, button) {
    const label = button.textContent;
    try {
      if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(value);
      button.textContent = t("Copiado ✓", "Copied ✓");
      setTimeout(() => { button.textContent = label; }, 1400);
    } catch {
      $("#diagnostic").textContent = t("Cópia automática indisponível. Selecione o texto e use Ctrl/Cmd+C.", "Automatic copy unavailable. Select the text and use Ctrl/Cmd+C.");
      if (button.id === "copy-button") { editor.focus(); editor.select(); }
    }
  }
  editor.addEventListener("input", input);
  editor.addEventListener("scroll", syncScroll);
  editor.addEventListener("select", syncEditor);
  editor.addEventListener("keydown", event => {
    if (event.isComposing) return;
    const command = event.metaKey || event.ctrlKey;
    if (command && (event.key.toLowerCase() === "z" || event.key.toLowerCase() === "y")) {
      event.preventDefault();
      undo(event.shiftKey || event.key.toLowerCase() === "y" ? 1 : -1); return;
    }
    if (command && event.key === "Enter") { event.preventDefault(); analyze(); return; }
    if (event.key === "Enter") {
      event.preventDefault();
      const start = editor.selectionStart;
      const line = editor.value.slice(0, start).split("\n").at(-1);
      const indent = line.match(/^\s*/)[0];
      const opensBlock = line.trimEnd().endsWith("{");
      const insertion = "\n" + indent + (opensBlock ? "    " : "");
      const closesBlock = opensBlock && editor.value[editor.selectionEnd] === "}";
      editor.setRangeText(insertion + (closesBlock ? "\n" + indent : ""), start, editor.selectionEnd, "end");
      editor.setSelectionRange(start + insertion.length, start + insertion.length);
      input();
      return;
    }
    if (event.key === "Escape") { tabInserts = false; $("#indent-toggle").checked = false; }
    if (event.key === "Tab" && tabInserts) {
      event.preventDefault();
      const start = editor.selectionStart, end = editor.selectionEnd;
      if (event.shiftKey || start !== end) {
        const lineStart = editor.value.lastIndexOf("\n", start - 1) + 1;
        const selected = editor.value.slice(lineStart, end);
        const replacement = selected.split("\n").map(line => event.shiftKey ? line.replace(/^( {1,4}|\t)/, "") : "    " + line).join("\n");
        editor.setRangeText(replacement, lineStart, end, "select");
      } else editor.setRangeText("    ", start, end, "end");
      input();
    }
  });
  $("#indent-toggle").addEventListener("change", event => { tabInserts = event.target.checked; });
  $("#run-button").addEventListener("click", analyze);
  $("#reset-button").addEventListener("click", () => {
    editor.value = lessons[current].code; editor.scrollTop = 0; input();
    $("#diagnostic").textContent = t("Original restaurado. Ctrl/Cmd+Z desfaz.", "Original restored. Ctrl/Cmd+Z to undo.");
  });
  $("#copy-button").addEventListener("click", event => copy(editor.value, event.currentTarget));
  $("#copy-cli").addEventListener("click", event => copy($("#cli-command").textContent, event.currentTarget));
  $("#download-button").addEventListener("click", () => {
    const url = URL.createObjectURL(new Blob([editor.value + "\n"], { type: "text/plain;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url; link.download = lessons[current].file; document.body.append(link); link.click(); link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  });
  $("#previous-button").addEventListener("click", () => loadLesson(current - 1, true));
  $("#next-button").addEventListener("click", () => loadLesson(current + 1, true));
  $("#lesson-search").addEventListener("input", renderNav);
  $("#complete-button").addEventListener("click", () => {
    const id = lessons[current].id;
    state.completed = state.completed.includes(id) ? state.completed.filter(value => value !== id) : [...state.completed, id];
    progress(); renderNav(); save();
  });
  $("#theme-toggle").addEventListener("click", () => {
    const dark = document.documentElement.classList.toggle("dark");
    $("#theme-toggle").setAttribute("aria-pressed", String(dark));
    try { storage.setItem("mhl-theme", dark ? "dark" : "light"); } catch { /* Theme still works. */ }
  });
  let theme;
  try { theme = storage.getItem("mhl-theme"); } catch { /* Use system preference. */ }
  const dark = theme === "dark" || (!theme && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  $("#theme-toggle").setAttribute("aria-pressed", String(dark));
  window.addEventListener("pagehide", save);
  window.addEventListener("hashchange", () => {
    const index = lessons.findIndex(lesson => "#" + lesson.id === location.hash);
    if (index >= 0) loadLesson(index, true);
  });
  const linked = lessons.findIndex(lesson => "#" + lesson.id === location.hash);
  loadLesson(linked >= 0 ? linked : current);
})();
