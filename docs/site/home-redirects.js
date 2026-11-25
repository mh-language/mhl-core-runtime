/* Preserve links to sections that moved out of the homepage. */
(() => {
  const moved = {
    '#start': 'start.html',
    '#examples': 'examples.html',
    '#why': 'why-mhl.html',
    '#docs': 'docs.html',
    '#hero-code': 'examples.html#refine-code',
    '#choose-code': 'examples.html#panel-workflow',
  };

  for (const example of ['workflow', 'limits', 'fanout', 'parallel', 'approval', 'contracts']) {
    for (const original of [`#panel-${example}`, `#tab-${example}`, `#${example}-code`]) {
      moved[original] = `examples.html#panel-${example}`;
    }
  }
  for (const id of ['install-unix', 'install-windows', 'hello-code', 'run-command']) {
    moved['#' + id] = 'start.html#' + id;
  }

  const redirect = () => {
    const target = moved[location.hash];
    if (target) location.replace(target);
  };
  redirect();
  window.addEventListener('hashchange', redirect);
})();
