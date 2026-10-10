// multiselect.js — the `multiSelect` Alpine component rendered by components.MultiSelect.
//
// `selected` is the list of chosen values (strings) and is x-modelable. Options are server
// rendered ([role=option] with data-value and data-label); `extra` lists selected values the
// options no longer offer.
(() => {
  const MAX_POPOVER_HEIGHT = 280;
  const EDGE = 8;

  const toggleValue = (selected, value) =>
    selected.includes(value) ? selected.filter((v) => v !== value) : [...selected, value];

  const matchesQuery = (label, query) => label.toLowerCase().includes(query.trim().toLowerCase());

  // step wraps around the visible values; from no current value, down starts at the top and
  // up at the bottom.
  const step = (values, current, delta) => {
    if (values.length === 0) return null;
    const i = values.indexOf(current);
    if (i === -1) return delta > 0 ? values[0] : values[values.length - 1];
    return values[(i + delta + values.length) % values.length];
  };

  window.CzMultiSelect = { toggleValue, matchesQuery, step };

  // x-show reveals on a later animation frame than $nextTick, so focus retries until shown.
  const focusWhenShown = (el, tries = 5) => {
    if (!el) return;
    el.focus();
    if (document.activeElement !== el && tries > 0) requestAnimationFrame(() => focusWhenShown(el, tries - 1));
  };

  document.addEventListener('alpine:init', () => {
    Alpine.data('multiSelect', () => ({
      selected: [],
      extra: [],
      open: false,
      query: '',
      active: null,
      popStyle: '',
      uid: '',
      unbind: null,

      get hasMatch() {
        const q = this.query;
        return (
          Array.from(this.$refs.list.querySelectorAll('[role=option]')).some((o) => matchesQuery(o.dataset.label, q)) ||
          this.extra.some((o) => matchesQuery(o.label, q))
        );
      },

      get activeID() {
        return this.open && this.active !== null ? `${this.uid}-opt-${this.active}` : null;
      },

      init() {
        this.uid = this.$refs.trigger.id;
        const follow = () => {
          if (this.open) this.place();
        };
        window.addEventListener('scroll', follow, true);
        window.addEventListener('resize', follow);
        const dialog = this.$el.closest('dialog');
        // Inside a modal <dialog>, Escape that reaches the dialog closes only the popover.
        const onCancel = (e) => {
          if (!this.open) return;
          e.preventDefault();
          this.close();
        };
        const onClose = () => (this.open = false);
        if (dialog) {
          dialog.addEventListener('cancel', onCancel);
          dialog.addEventListener('close', onClose);
        }
        this.unbind = () => {
          window.removeEventListener('scroll', follow, true);
          window.removeEventListener('resize', follow);
          if (dialog) {
            dialog.removeEventListener('cancel', onCancel);
            dialog.removeEventListener('close', onClose);
          }
        };
        this.$watch('query', () => {
          if (!this.open) return;
          const visible = this.visibleValues();
          if (!visible.includes(this.active)) this.active = visible[0] ?? null;
          this.$nextTick(() => this.placeWhenShown());
        });
      },

      destroy() {
        if (this.unbind) this.unbind();
      },

      has(value) {
        return this.selected.includes(String(value));
      },

      matches(label) {
        return matchesQuery(label, this.query);
      },

      isActive(value) {
        return this.active === String(value);
      },

      checked(el) {
        const option = el.closest('[role=option]');
        return option ? this.has(option.dataset.value) : false;
      },

      visibleValues() {
        return Array.from(this.$refs.list.querySelectorAll('[role=option]'))
          .filter((o) => matchesQuery(o.dataset.label, this.query))
          .map((o) => o.dataset.value);
      },

      toggleOpen() {
        if (this.open) this.close();
        else this.show();
      },

      toggleOption(value) {
        value = String(value);
        this.selected = toggleValue(this.selected, value);
        this.active = value;
      },

      show() {
        if (this.open) return;
        this.query = '';
        this.open = true;
        this.$nextTick(() => {
          const visible = this.visibleValues();
          this.active = visible.find((v) => this.has(v)) ?? visible[0] ?? null;
          this.placeWhenShown();
          focusWhenShown(this.$refs.filter || this.$refs.list);
          this.reveal();
        });
      },

      close(returnFocus = true) {
        if (!this.open) return;
        this.open = false;
        if (returnFocus) this.$refs.trigger.focus();
      },

      move(delta) {
        this.active = step(this.visibleValues(), this.active, delta);
        this.reveal();
      },

      reveal() {
        this.$nextTick(() => {
          if (this.active === null) return;
          const list = this.$refs.list;
          const li = list.querySelector(`[role=option][data-value="${CSS.escape(this.active)}"]`);
          if (!li) return;
          // scrollIntoView would also scroll the modal body behind this fixed popover.
          const lr = list.getBoundingClientRect();
          const r = li.getBoundingClientRect();
          if (r.top < lr.top) list.scrollTop -= lr.top - r.top;
          else if (r.bottom > lr.bottom) list.scrollTop += r.bottom - lr.bottom;
        });
      },

      onKey(e) {
        const inFilter = e.target === this.$refs.filter;
        if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
          e.preventDefault();
          this.move(e.key === 'ArrowDown' ? 1 : -1);
        } else if (e.key === 'Home' || e.key === 'End') {
          if (inFilter) return;
          e.preventDefault();
          const visible = this.visibleValues();
          this.active = e.key === 'Home' ? visible[0] ?? null : visible[visible.length - 1] ?? null;
          this.reveal();
        } else if (e.key === 'Enter' || (e.key === ' ' && !inFilter)) {
          // Enter must not submit the card form.
          e.preventDefault();
          if (this.active !== null) this.toggleOption(this.active);
        }
      },

      // x-show reveals on a later animation frame than $nextTick, and a hidden list measures 0.
      placeWhenShown(tries = 5) {
        if (this.$refs.list.scrollHeight === 0 && tries > 0) {
          requestAnimationFrame(() => this.placeWhenShown(tries - 1));
          return;
        }
        this.place();
      },

      // position:fixed keeps the popover out of the modal body's overflow clipping; it opens on
      // whichever side of the trigger has more room, and its list scrolls within that room.
      place() {
        const r = this.$refs.trigger.getBoundingClientRect();
        const filterHeight = this.$refs.filterBox ? this.$refs.filterBox.offsetHeight : 0;
        const want = Math.min(MAX_POPOVER_HEIGHT, filterHeight + this.$refs.list.scrollHeight + 2);
        const below = window.innerHeight - r.bottom - EDGE - 4;
        const above = r.top - EDGE - 4;
        const down = below >= want || below >= above;
        const height = Math.max(0, Math.min(want, down ? below : above));
        const top = down ? r.bottom + 4 : r.top - 4 - height;
        const left = Math.max(EDGE, Math.min(r.left, window.innerWidth - r.width - EDGE));
        this.popStyle = `position:fixed;left:${left}px;top:${top}px;width:${r.width}px;max-height:${height}px`;
      },
    }));
  });
})();
