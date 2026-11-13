// Run page: log search, timestamps toggle, and opening the step that holds a #L<n> anchor.
(function () {
  var query = '';

  function openAnchor() {
    var m = /^#(L\d+)$/.exec(location.hash);
    if (!m) return;
    var row = document.getElementById(m[1]);
    if (!row) return;
    var d = row.closest('details');
    if (d) d.open = true;
    row.scrollIntoView({ block: 'center' });
  }

  function clearMarks() {
    document.querySelectorAll('[data-line] mark').forEach(function (m) {
      m.replaceWith(document.createTextNode(m.textContent));
    });
    document.querySelectorAll('[data-line]').forEach(function (td) { td.normalize(); });
  }

  function applySearch() {
    clearMarks();
    var count = 0, q = query.toLowerCase();
    if (q) {
      document.querySelectorAll('[data-line]').forEach(function (td) {
        var walker = document.createTreeWalker(td, NodeFilter.SHOW_TEXT), nodes = [], n;
        while ((n = walker.nextNode())) nodes.push(n);
        nodes.forEach(function (node) {
          var t = node.nodeValue, lower = t.toLowerCase(), i = lower.indexOf(q);
          if (i < 0) return;
          var frag = document.createDocumentFragment(), last = 0;
          while (i >= 0) {
            frag.appendChild(document.createTextNode(t.slice(last, i)));
            var mark = document.createElement('mark');
            mark.className = 'bg-warning/30 text-foreground rounded-xs';
            mark.textContent = t.slice(i, i + q.length);
            frag.appendChild(mark);
            count++;
            last = i + q.length;
            i = lower.indexOf(q, last);
          }
          frag.appendChild(document.createTextNode(t.slice(last)));
          node.parentNode.replaceChild(frag, node);
        });
        var d = td.closest('details');
        if (d && td.querySelector('mark')) d.open = true;
      });
    }
    var c = document.querySelector('[data-match-count]');
    if (c) c.textContent = q ? count + (count === 1 ? ' match' : ' matches') : '';
  }

  document.addEventListener('input', function (e) {
    if (e.target.id !== 'log-search') return;
    query = e.target.value;
    applySearch();
  });

  document.addEventListener('click', function (e) {
    var t = e.target.closest('[data-ts-toggle]');
    if (!t) return;
    var on = t.getAttribute('aria-pressed') !== 'true';
    t.setAttribute('aria-pressed', String(on));
    document.querySelectorAll('[data-ts]').forEach(function (el) { el.hidden = !on; });
  });

  // The live tail appends rows under a search that is still active; mark them too.
  var steps = document.getElementById('job-steps');
  if (steps) {
    var observer = new MutationObserver(function () {
      if (!query) return;
      applySearch();
      // applySearch's own DOM edits are queued as records; dropping them stops the observer re-triggering itself.
      observer.takeRecords();
    });
    observer.observe(steps, { childList: true, subtree: true });
  }

  window.addEventListener('hashchange', openAnchor);
  openAnchor();
})();
