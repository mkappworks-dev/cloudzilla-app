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
      this.active = Math.max(0, this.options().findIndex(o => o.dataset.value === this.value));
      this.$nextTick(() => {
        this.$refs.list.focus();
        this.reveal();
      });
    },
    hide(refocus) {
      this.open = false;
      if (refocus) this.$refs.trigger.focus();
    },
    move(delta) {
      const n = this.options().length;
      this.active = (this.active + delta + n) % n;
      this.reveal();
    },
    reveal() {
      this.options()[this.active]?.scrollIntoView({ block: 'nearest' });
    },
    choose(i) {
      const opt = this.options()[i];
      if (!opt) return;
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
      const i = this.options().findIndex(o => o.textContent.trim().toLowerCase().startsWith(this.typed));
      if (i >= 0) {
        this.active = i;
        this.reveal();
      }
    },
    onKey(e) {
      switch (e.key) {
        case 'ArrowDown': e.preventDefault(); this.move(1); break;
        case 'ArrowUp': e.preventDefault(); this.move(-1); break;
        case 'Home': e.preventDefault(); this.active = 0; this.reveal(); break;
        case 'End': e.preventDefault(); this.active = this.options().length - 1; this.reveal(); break;
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
