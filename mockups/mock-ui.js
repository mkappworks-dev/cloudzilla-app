/* Interaction for the generated mocks. They carry no htmx or Alpine and send no requests, so what a control does is
 * drawn here, once, for every page. Add a behaviour only when a generated page needs it to show a state. */
(function () {
  var html = document.documentElement;

  function syncThemeUI() {
    var isLight = html.classList.contains('light');
    document.querySelectorAll('.dark-only').forEach(function (el) { el.classList.toggle('hidden', isLight); });
    document.querySelectorAll('.light-only').forEach(function (el) { el.classList.toggle('hidden', !isLight); });
    var toggle = document.getElementById('theme-toggle');
    if (toggle) {
      toggle.setAttribute('aria-pressed', String(isLight));
      toggle.setAttribute('aria-label', isLight ? 'Switch to dark theme' : 'Switch to light theme');
    }
  }

  document.addEventListener('click', function (e) {
    if (!e.target.closest('#theme-toggle')) return;
    html.classList.toggle('light');
    html.classList.toggle('dark', !html.classList.contains('light'));
    try { localStorage.setItem('cz-theme', html.classList.contains('light') ? 'light' : 'dark'); } catch (err) { /* storage blocked */ }
    syncThemeUI();
  });
  syncThemeUI();

  // Tablists (milestones): the real page switches them with an inline script the generator strips.
  document.querySelectorAll('[role="tablist"]').forEach(function (list) {
    var tabs = Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));
    if (!tabs.length || !tabs[0].getAttribute('aria-controls')) return;
    function show(active) {
      tabs.forEach(function (tab) {
        var on = tab === active;
        document.getElementById(tab.getAttribute('aria-controls')).classList.toggle('hidden', !on);
        tab.setAttribute('aria-selected', String(on));
        tab.tabIndex = on ? 0 : -1;
        tab.classList.toggle('border-foreground', on);
        tab.classList.toggle('text-foreground', on);
        tab.classList.toggle('font-medium', on);
        tab.classList.toggle('border-transparent', !on);
        tab.classList.toggle('text-muted-foreground', !on);
      });
    }
    tabs.forEach(function (tab, i) {
      tab.addEventListener('click', function () { show(tab); });
      tab.addEventListener('keydown', function (e) {
        var next = e.key === 'Home' ? 0 : e.key === 'End' ? tabs.length - 1
          : e.key === 'ArrowRight' ? (i + 1) % tabs.length : e.key === 'ArrowLeft' ? (i + tabs.length - 1) % tabs.length : -1;
        if (next < 0) return;
        e.preventDefault();
        show(tabs[next]);
        tabs[next].focus();
      });
    });
  });
})();
