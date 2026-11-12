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
})();
