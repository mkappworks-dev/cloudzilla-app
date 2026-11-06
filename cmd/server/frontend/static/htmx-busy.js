// htmx-busy.js — disables the control behind each htmx request and marks it aria-busy until the request settles.
// htmx 4 queues a repeat trigger rather than dropping it, so without this a second click on Merge sends a second merge.
(() => {
  const pending = new Set();

  // mark disables the given buttons and returns a function that re-enables them. A button that was already disabled is
  // left alone, so the release can't enable one the page disabled for its own reasons.
  const mark = (controls) => {
    const marked = controls.filter((el) => el && !el.disabled);
    const focused = marked.includes(document.activeElement) ? document.activeElement : null;
    for (const el of marked) {
      el.disabled = true;
      el.setAttribute('aria-busy', 'true');
    }
    const release = () => {
      pending.delete(release);
      for (const el of marked) {
        el.disabled = false;
        el.removeAttribute('aria-busy');
      }
      // Disabling blurs the button; hand focus back so a keyboard user can retry after a failure.
      const active = document.activeElement;
      if (focused && focused.isConnected && (!active || active === document.body)) focused.focus();
    };
    pending.add(release);
    return release;
  };

  window.CzBusy = { mark };

  const requestControls = (ctx) => {
    const elt = ctx.sourceElement;
    if (elt.matches('button, input[type="submit"]')) return [elt];
    // A filter or live-search form also fires on input or change; disabling its buttons per keystroke would flicker.
    if (elt.tagName === 'FORM' && ctx.sourceEvent?.type === 'submit') {
      return Array.from(elt.elements).filter((el) => el.type === 'submit');
    }
    return [];
  };

  const releases = new WeakMap();

  document.addEventListener('htmx:before:request', (e) => {
    const ctx = e.detail.ctx;
    const controls = requestControls(ctx);
    if (controls.length) releases.set(ctx, mark(controls));
  });

  // finally:request, not after:request: htmx skips after:request when the fetch itself throws.
  document.addEventListener('htmx:finally:request', (e) => {
    const ctx = e.detail.ctx;
    // The page is about to unload; re-enabling now would open a window for a second click.
    if (ctx.hx?.redirect || ctx.hx?.refresh === 'true') return;
    releases.get(ctx)?.();
  });

  // A page restored from the back/forward cache comes back with the buttons a redirect left disabled.
  window.addEventListener('pageshow', (e) => {
    if (e.persisted) for (const release of [...pending]) release();
  });
})();
