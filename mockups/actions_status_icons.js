/* One status icon set for every CI surface in these mocks. Mark up <span data-status="failure"></span>
 * (optional data-size) and it is filled in; czStatusIcon(status, size) returns the same SVG as a string. */
(function () {
  var SET = {
    queued:    { label: 'Queued',    cls: 'text-muted-foreground', body: '<circle cx="12" cy="12" r="10"></circle><path d="M12 7v5l3 2"></path>' },
    waiting:   { label: 'Approval required', cls: 'text-warning', body: '<circle cx="12" cy="12" r="10"></circle><path d="M10 9v6M14 9v6"></path>' },
    running:   { label: 'Running',   cls: 'text-foreground motion-safe:animate-pulse', body: '<circle cx="12" cy="12" r="10"></circle><circle cx="12" cy="12" r="3.5" fill="currentColor"></circle>' },
    success:   { label: 'Succeeded', cls: 'text-success', body: '<circle cx="12" cy="12" r="10"></circle><path d="M9 12l2 2 4-4"></path>' },
    failure:   { label: 'Failed',    cls: 'text-destructive', body: '<circle cx="12" cy="12" r="10"></circle><path d="M15 9l-6 6M9 9l6 6"></path>' },
    cancelled: { label: 'Cancelled', cls: 'text-muted-foreground', body: '<circle cx="12" cy="12" r="10"></circle><path d="M4.93 4.93l14.14 14.14"></path>' },
    skipped:   { label: 'Skipped',   cls: 'text-muted-foreground', body: '<circle cx="12" cy="12" r="10"></circle><path d="M8 12h8"></path>' }
  };
  function icon(status, size, extraClass) {
    var d = SET[status];
    size = size || 14;
    return '<svg width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
      'class="shrink-0 ' + d.cls + (extraClass ? ' ' + extraClass : '') + '" role="img" aria-label="' + d.label + '">' + d.body + '</svg>';
  }
  function fill(root) {
    (root || document).querySelectorAll('[data-status]:empty').forEach(function (el) {
      el.innerHTML = icon(el.dataset.status, +el.dataset.size || 14, el.dataset.iconClass);
      el.classList.add('inline-flex');
    });
  }
  window.czStatusIcon = icon;
  window.czStatusLabel = function (s) { return SET[s].label; };
  window.czFillStatusIcons = fill;
  fill();
})();
