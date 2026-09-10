(function (root) {
  "use strict";
  const keywords = new Set("agent prompt pipeline workflow loop parallel step input output memory mem tool extension extensible test describe var const enum type if else match while for in return break import from export as true false null max spawn wait any of timeout try catch finally goto".split(" "));
  const types = new Set("string number bool object array duration datetime secret".split(" "));
  const escapeHtml = value => value.replace(/[&<>"]/g, ch => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[ch]));

  // Tokenize source before producing markup. Never apply replacements to HTML.
  function tokenize(source) {
    const tokens = [];
    let offset = 0;
    while (offset < source.length) {
      const rest = source.slice(offset);
      let value, kind = "", unclosed = false;
      if (rest.startsWith("//")) {
        value = rest.match(/^[^\n]*/)[0]; kind = "comment";
      } else if (rest.startsWith("/*")) {
        const end = source.indexOf("*/", offset + 2);
        unclosed = end < 0;
        value = source.slice(offset, unclosed ? source.length : end + 2); kind = "comment";
      } else if (rest.startsWith('"')) {
        const delimiter = rest.startsWith('"""') ? '"""' : '"';
        let end = offset + delimiter.length;
        let closed = false;
        while (end < source.length) {
          if (source[end] === "\\") { end += 2; continue; }
          if (source.startsWith(delimiter, end)) { end += delimiter.length; closed = true; break; }
          end++;
        }
        value = source.slice(offset, end); kind = "string"; unclosed = !closed;
      } else if ((value = rest.match(/^\d+(?:\.\d+)?(?:ms|s|m|h|d)?\b/)?.[0])) {
        kind = "number";
      } else if ((value = rest.match(/^[A-Za-z_]\w*/)?.[0])) {
        if (keywords.has(value)) kind = "keyword";
        else if (types.has(value)) kind = "type";
        else if (/^\s*\(/.test(rest.slice(value.length))) kind = "call";
        else if (/^\s*:/.test(rest.slice(value.length)) || tokens.at(-1)?.value === ".") kind = "property";
      } else if ((value = rest.match(/^(?:->|\?\?|\?\.|==|!=|>=|<=|&&|\|\||\+=|[+*/%=!<>-])/)?.[0])) {
        kind = "operator";
      } else {
        value = rest.match(/^\s+/)?.[0] || rest[0];
      }
      tokens.push({ value, kind, offset, unclosed });
      offset += value.length;
    }
    return tokens;
  }
  function highlight(source) {
    return tokenize(source).map(token => token.kind
      ? '<span class="tok-' + token.kind + '">' + escapeHtml(token.value) + "</span>"
      : escapeHtml(token.value)).join("");
  }
  function diagnostics(source, locale = "pt-BR") {
    const t = (pt, en) => locale === "en" ? en : pt;
    const errors = [], stack = [], pairs = { "}": "{", "]": "[", ")": "(" };
    function add(message, offset) {
      const before = source.slice(0, offset).split("\n");
      errors.push({ message, offset, line: before.length, column: before.at(-1).length + 1 });
    }
    if (!source.trim()) add(t("O editor está vazio.", "The editor is empty."), 0);
    for (const token of tokenize(source)) {
      if (token.unclosed) add(token.kind === "string" ? t("String sem fechamento.", "Unclosed string.") : t("Comentário sem fechamento.", "Unclosed comment."), token.offset);
      if (token.kind === "string" || token.kind === "comment") continue;
      if ("{[(".includes(token.value)) stack.push(token);
      if (pairs[token.value]) {
        if (stack.at(-1)?.value === pairs[token.value]) stack.pop();
        else add(t("Delimitador inesperado: ", "Unexpected delimiter: ") + token.value, token.offset);
      }
    }
    for (const token of stack) add(t("Delimitador sem fechamento: ", "Unclosed delimiter: ") + token.value, token.offset);
    return errors.sort((a, b) => a.offset - b.offset);
  }
  function readState(storage, lessons, key = "mhl-playground-v2") {
    const clean = { current: lessons[0].id, completed: [], drafts: {} };
    try {
      const data = JSON.parse(storage.getItem(key));
      if (!data || typeof data !== "object") return clean;
      const ids = new Set(lessons.map(lesson => lesson.id));
      if (ids.has(data.current)) clean.current = data.current;
      if (Array.isArray(data.completed)) clean.completed = [...new Set(data.completed.filter(id => ids.has(id)))];
      for (const id of ids) {
        if (data.drafts && typeof data.drafts[id] === "string") clean.drafts[id] = data.drafts[id];
      }
    } catch { /* Private mode, blocked storage or damaged state must not block editing. */ }
    return clean;
  }
  const api = { escapeHtml, tokenize, highlight, diagnostics, readState };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  else root.MhlPlayground = api;
})(globalThis);
