// Stand-in for components.SignatureBadge: swaps each <span data-signature="state"></span> for the badge and its
// popover, with the markup and classes of internal/view/components/signature_badge.templ. The real popover is Alpine;
// this one toggles `hidden`. States: verified-ssh, verified-gpg, instance, partial, unverified. Add data-floating for
// SignatureBadgeFloating (fixed popover) and data-icon for SignatureIcon (glyph only, fixed popover).
(function () {
  var SHIELD = '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path>';
  var ICON = {
    success: ['text-success', SHIELD + '<path d="M9 12l2 2 4-4"></path>'],
    outline: ['text-muted-foreground', SHIELD],
    warning: ['text-warning', '<path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line>'],
  };
  var BTN = {
    success: 'bg-success/15 text-success border-success/30 hover:bg-success/25',
    outline: 'border-border text-foreground hover:bg-accent',
    warning: 'bg-warning/15 text-warning border-warning/30 hover:bg-warning/25',
  };
  function signer(initials, user, extra) {
    return '<div class="mt-3 flex items-center gap-2"><span class="inline-grid place-items-center rounded-full bg-muted border border-border font-medium text-muted-foreground h-5 w-5 text-[10px]" role="img" aria-label="' + user + '">' + initials + '</span>' +
      (user === 'Cloudzilla' ? '<span class="text-[13px] font-medium">Cloudzilla</span>' : '<a href="#" class="text-[13px] font-medium hover:underline">' + user + '</a>') +
      (extra ? '<span class="text-[12px] text-muted-foreground truncate">' + extra + '</span>' : '') + '</div>';
  }
  function dl(rows) {
    return '<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-[12px]">' + rows.map(function (r) {
      return '<dt class="text-muted-foreground">' + r[0] + '</dt><dd' + (r[2] ? ' class="font-mono break-all"' : '') + '>' + r[1] + '</dd>';
    }).join('') + '</dl>';
  }
  var S = {
    'verified-ssh': { tone: 'success', label: 'Verified', head: 'Signed by the committer with a key on their account.', who: signer('M', 'malith', 'malith@example.com'),
      rows: [['Key type', 'SSH'], ['Key', 'MacBook Pro'], ['Fingerprint', 'SHA256:Zk1p7vQ3nXh0e9LwYb2sAq5cT8uRfJdM4oGiNxVa6Hw', 1]] },
    'verified-gpg': { tone: 'success', label: 'Verified', head: 'Signed by the committer with a key on their account.', who: signer('D', 'daisy', 'daisy@example.com'),
      rows: [['Key type', 'GPG'], ['Key ID', '3AA5C34371567BD2', 1], ['Fingerprint', '4AEE18F83AFDEB23B1C97D043AA5C34371567BD2', 1]] },
    instance: { tone: 'success', label: 'Verified', head: 'This commit was made on the web and signed by this server.', who: signer('C', 'Cloudzilla', ''),
      rows: [['Fingerprint', 'SHA256:q8Wm3TzR7cYb1sVx0nLk4eJdA2oFiNyHa5Kt9Ep6UgB', 1]] },
    partial: { tone: 'outline', label: 'Partially verified', head: 'The signature is valid and the key is rhea\'s, but the committer email isn\'t one of rhea\'s verified emails.', who: signer('R', 'rhea', 'committed as rhea@contractor.dev'),
      rows: [['Key type', 'SSH'], ['Key', 'YubiKey 5C'], ['Fingerprint', 'SHA256:p0Qe2Vx8nLk4TzR7cYb1sWm9uHgJdA3oFiNyVa5Kt2E', 1]],
      hint: 'Verifying rhea@contractor.dev on rhea\'s account makes this Verified.' },
    unverified: { tone: 'warning', label: 'Unverified', head: 'The commit is signed, but no account on this server has the key.', who: '',
      rows: [['Key type', 'GPG'], ['Key ID', 'B5690EEEBB952194', 1], ['Committer', 'kiyo@example.com']],
      hint: 'If this is your key, add it under Settings → Signing keys. Commits re-verify on the next view.' },
  };

  function build(host, state) {
    var d = S[state], icon = ICON[d.tone];
    var glyph = function (size, cls) {
      return '<svg width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="' + cls + '" aria-hidden="true" focusable="false">' + icon[1] + '</svg>';
    };
    var isIcon = 'icon' in host.dataset, floating = isIcon || 'floating' in host.dataset;
    host.outerHTML =
      '<div class="relative inline-block" data-sig>' +
        '<button type="button" data-sig-btn aria-expanded="false" aria-haspopup="dialog" ' + (isIcon ? 'aria-label="' + d.label + ' signature" title="' + d.label + '" class="inline-flex items-center justify-center h-5 w-5 rounded-full border ' : 'class="inline-flex items-center gap-1.5 h-[22px] px-2 rounded-full border text-[11px] font-medium ') + BTN[d.tone] + '">' + (isIcon ? glyph(12, '') : d.label) + '</button>' +
        '<div hidden data-sig-panel role="dialog" aria-label="Commit signature" class="' + (floating ? 'fixed' : 'absolute left-0 sm:left-auto sm:right-0 top-full mt-1.5') + ' z-50 w-[340px] max-w-[calc(100vw-3rem)] rounded-md border border-border bg-popover text-popover-foreground shadow-md p-3 text-left font-normal whitespace-normal">' +
          '<p class="flex items-start gap-2 text-[13px] font-medium">' + glyph(14, 'shrink-0 mt-[3px] ' + icon[0]) + '<span>' + d.head + '</span></p>' +
          d.who + dl(d.rows) + (d.hint ? '<p class="mt-2 text-[12px] text-muted-foreground">' + d.hint + '</p>' : '') +
          '<div role="separator" class="my-2 h-px bg-border"></div><a href="account_settings.html" class="text-[12px] text-muted-foreground hover:text-foreground">About signing keys →</a>' +
        '</div></div>';
  }
  document.querySelectorAll('[data-signature]').forEach(function (h) { build(h, h.dataset.signature); });

  function close(w) {
    w.querySelector('[data-sig-panel]').hidden = true;
    w.querySelector('[data-sig-btn]').setAttribute('aria-expanded', 'false');
  }
  document.querySelectorAll('[data-sig]').forEach(function (w) {
    var btn = w.querySelector('[data-sig-btn]'), panel = w.querySelector('[data-sig-panel]');
    btn.addEventListener('click', function () {
      var open = panel.hidden;
      if (open && panel.classList.contains('fixed')) {
        var b = btn.getBoundingClientRect(), w = Math.min(340, window.innerWidth - 48);
        panel.style.top = b.bottom + 6 + 'px';
        panel.hidden = false;
        var h = panel.offsetHeight;
        if (b.bottom + 6 + h > window.innerHeight - 8) panel.style.top = Math.max(8, b.top - 6 - h) + 'px';
        panel.style.left = Math.max(8, Math.min(b.left, window.innerWidth - w - 8)) + 'px';
      }
      document.querySelectorAll('[data-sig]').forEach(close);
      panel.hidden = !open;
      btn.setAttribute('aria-expanded', String(open));
    });
  });
  function closeFloating() {
    document.querySelectorAll('[data-sig]').forEach(function (w) {
      if (w.querySelector('[data-sig-panel]').classList.contains('fixed')) close(w);
    });
  }
  window.addEventListener('scroll', closeFloating, true);
  window.addEventListener('resize', closeFloating);
  document.addEventListener('click', function (e) {
    document.querySelectorAll('[data-sig]').forEach(function (w) { if (!w.contains(e.target)) close(w); });
  });
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Escape') return;
    document.querySelectorAll('[data-sig]').forEach(function (w) {
      if (!w.querySelector('[data-sig-panel]').hidden) { close(w); w.querySelector('[data-sig-btn]').focus(); }
    });
  });
})();
