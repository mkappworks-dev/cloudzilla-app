document.addEventListener('alpine:init', () => {
  Alpine.data('selectMenu', () => ({
    open: false,
    active: -1,
    value: '',
    typed: '',
    typedAt: 0,

    init() {
      this.value = this.$refs.input.value;
    },
    options() {
      return [...this.$refs.list.querySelectorAll('[role=option]')];
    },
    optionID(i) {
      return this.options()[i]?.id ?? null;
    },
    show() {
      this.open = true;
      const current = this.options().findIndex(o => o.dataset.value === this.value);
      this.active = current >= 0 ? current : this.step(-1, 1);
      this.$nextTick(() => {
        this.$refs.list.focus();
        this.reveal();
      });
    },
    hide(refocus) {
      this.open = false;
      if (refocus) this.$refs.trigger.focus();
    },
    enabled(i) {
      return this.options()[i]?.getAttribute('aria-disabled') !== 'true';
    },
    // Disabled options stay visible with their reason but take no focus or choice; a list of only disabled ones keeps `from`.
    step(from, delta) {
      const n = this.options().length;
      for (let i = 1; i <= n; i++) {
        const next = (from + delta * i + n * i) % n;
        if (this.enabled(next)) return next;
      }
      return from;
    },
    move(delta) {
      this.active = this.step(this.active, delta);
      this.reveal();
    },
    reveal() {
      this.options()[this.active]?.scrollIntoView({ block: 'nearest' });
    },
    choose(i) {
      const opt = this.options()[i];
      if (!opt || !this.enabled(i)) return;
      this.hide(true);
      if (opt.dataset.value === this.value) return;
      this.value = opt.dataset.value;
      this.options().forEach(o => o.setAttribute('aria-selected', String(o === opt)));
      this.$refs.current.replaceChildren(...opt.querySelector('[data-option-body]').cloneNode(true).childNodes);
      this.$refs.input.value = this.value;
      // Hidden inputs never fire change on their own; forms and onchange handlers listen for it.
      this.$refs.input.dispatchEvent(new Event('change', { bubbles: true }));
    },
    typeahead(key) {
      const now = Date.now();
      this.typed = (now - this.typedAt > 500 ? '' : this.typed) + key.toLowerCase();
      this.typedAt = now;
      const i = this.options().findIndex((o, j) => this.enabled(j) && o.textContent.trim().toLowerCase().startsWith(this.typed));
      if (i >= 0) {
        this.active = i;
        this.reveal();
      }
    },
    onKey(e) {
      switch (e.key) {
        case 'ArrowDown': e.preventDefault(); this.move(1); break;
        case 'ArrowUp': e.preventDefault(); this.move(-1); break;
        case 'Home': e.preventDefault(); this.active = this.step(-1, 1); this.reveal(); break;
        case 'End': e.preventDefault(); this.active = this.step(0, -1); this.reveal(); break;
        case 'Enter':
        case ' ': e.preventDefault(); this.choose(this.active); break;
        case 'Escape': e.preventDefault(); this.hide(true); break;
        case 'Tab': this.hide(false); break;
        default:
          if (e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey) this.typeahead(e.key);
      }
    },
  }));
});

// Keyboard behaviour for every role=menu popup, found by markup so each page's Alpine open-state stays as is.
(() => {
  const visible = el => el.getClientRects().length > 0;
  const items = menu => [...menu.querySelectorAll('[role=menuitem]')].filter(visible);

  // Nearest preceding aria-haspopup=menu button in an ancestor: the menu's trigger.
  const triggerOf = menu => {
    for (let a = menu.parentElement, hops = 0; a && hops < 5; a = a.parentElement, hops++) {
      const found = [...a.querySelectorAll('[aria-haspopup=menu]')].filter(t =>
        !menu.contains(t) && t.compareDocumentPosition(menu) & Node.DOCUMENT_POSITION_FOLLOWING);
      if (found.length) return found[found.length - 1];
    }
    return null;
  };
  const menuOf = trigger => {
    for (let a = trigger.parentElement, hops = 0; a && hops < 5; a = a.parentElement, hops++) {
      const m = [...a.querySelectorAll('[role=menu]')].find(m =>
        !m.contains(trigger) && trigger.compareDocumentPosition(m) & Node.DOCUMENT_POSITION_FOLLOWING);
      if (m) return m;
    }
    return null;
  };
  const isExpanded = t => t.getAttribute('aria-expanded') === 'true';

  const focusItem = (menu, pick) => {
    const list = items(menu);
    if (list.length) pick(list).focus();
  };
  // Alpine reveals the menu and updates aria-expanded a tick after the click that opened it.
  const openThen = (trigger, pick) => {
    if (!isExpanded(trigger)) trigger.click();
    setTimeout(() => {
      const menu = menuOf(trigger);
      if (menu) focusItem(menu, pick);
    }, 0);
  };
  // Menus differ in how they close on Escape (some already do it on window); toggling blindly would reopen them.
  const close = (trigger, refocus) => setTimeout(() => {
    if (isExpanded(trigger)) trigger.click();
    if (refocus) trigger.focus();
  }, 0);

  document.addEventListener('click', e => {
    const trigger = e.target.closest('[aria-haspopup=menu]');
    // detail 0 is a keyboard activation; a mouse click keeps focus where it is.
    if (trigger && e.detail === 0) setTimeout(() => {
      if (isExpanded(trigger)) { const m = menuOf(trigger); if (m) focusItem(m, l => l[0]); }
    }, 0);
  });

  document.addEventListener('keydown', e => {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const trigger = e.target.closest?.('[aria-haspopup=menu]');
    if (trigger && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      e.preventDefault();
      openThen(trigger, e.key === 'ArrowDown' ? l => l[0] : l => l[l.length - 1]);
      return;
    }
    const menu = e.target.closest?.('[role=menu]');
    if (!menu) return;
    const owner = triggerOf(menu);
    const list = items(menu);
    const at = list.indexOf(e.target);
    const go = i => { e.preventDefault(); if (list.length) list[(i + list.length) % list.length].focus(); };
    switch (e.key) {
      case 'ArrowDown': go(at + 1); break;
      case 'ArrowUp': go(at < 0 ? list.length - 1 : at - 1); break;
      case 'Home': if (at >= 0) go(0); break;
      case 'End': if (at >= 0) go(list.length - 1); break;
      case 'Escape': if (owner) close(owner, true); break;
      case 'Tab': if (owner && at >= 0) close(owner, false); break;
    }
  });
})();
