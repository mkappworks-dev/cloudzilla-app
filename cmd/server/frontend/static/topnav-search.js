document.addEventListener('alpine:init', () => {
  Alpine.data('topnavSearch', (listId = 'topnav-suggest') => {
    // Found once in init(): inside a handler $el is the input or button that fired it, and a
    // $ref would register with the field's own nested x-data scope instead of this one.
    let list;

    return {
      open: false,
      active: -1,

      init() {
        list = this.$el.querySelector('#' + listId);
        // htmx swaps the dropdown's contents; follow them instead of its events.
        new MutationObserver(() => {
          this.active = -1;
          this.open = this.options().length > 0;
          if (this.open) window.dispatchEvent(new CustomEvent('suggest-opened', { detail: listId }));
        }).observe(list, { childList: true });
        // The topnav and the results page each have a field; only one list shows at a time.
        window.addEventListener('suggest-opened', (e) => {
          if (e.detail !== listId) this.close();
        });
      },
      optionId(i) {
        return listId + '-' + i;
      },
      options() {
        return [...list.querySelectorAll('[data-suggest-item]')];
      },
      clear() {
        list.replaceChildren();
      },
      move(delta) {
        const n = this.options().length;
        if (!n) return;
        this.open = true;
        this.active = this.active < 0 ? (delta > 0 ? 0 : n - 1) : (this.active + delta + n) % n;
        this.options()[this.active].scrollIntoView({ block: 'nearest' });
      },
      follow(event) {
        const option = this.options()[this.active];
        if (!this.open || !option) return; // nothing highlighted: Enter submits the form
        event.preventDefault();
        option.click();
      },
      escape(event) {
        if (!this.open) return; // a closed field keeps the browser's own Escape (clears a search input)
        event.preventDefault();
        this.close();
      },
      close() {
        this.open = false;
        this.active = -1;
      },
      reopen() {
        this.open = this.options().length > 0;
      },
    };
  });
});

document.addEventListener('keydown', (e) => {
  if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
  const t = e.target;
  if (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName)) return;
  const input = document.getElementById('topnav-search');
  if (!input || input.offsetParent === null) return; // hidden below md: leave "/" to the browser
  e.preventDefault();
  input.focus();
  input.select();
});
