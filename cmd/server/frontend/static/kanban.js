// kanban.js — Alpine components for the project board.
//
// The board root declares x-data="kanbanBoard" with data-owner, data-repo-name,
// data-project-id and data-can-write. Cards carry data-drag="kanban-card" and
// data-card-id; columns carry data-drop="kanban-column", data-column-id and a
// [data-cards] list. Cards that open the card dialog carry data-card-open and
// data-card-json; their rendered description sits in <template id="desc-{id}">.
// Each column's "+ Add item" button carries data-add-card and data-column-name.
document.addEventListener('alpine:init', () => {
  const projectURL = (el, path) => {
    const root = el.closest('[data-project-id]');
    return `/api/repos/${root.dataset.owner}/${root.dataset.repoName}/projects/${root.dataset.projectId}${path}`;
  };

  const send = async (method, url, body) => {
    const csrf = (document.cookie.match(/csrf_token=([^;]+)/) || [])[1] || '';
    const r = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!r.ok) {
      const detail = await r.json().then((j) => j.error, () => '');
      throw new Error(detail || 'Request failed (' + r.status + ')');
    }
    return r;
  };

  // x-show reveals an element on a later animation frame than $nextTick, so focus retries
  // until the element is displayed.
  const focusWhenShown = (el, tries = 5) => {
    if (!el) return;
    el.focus();
    if (document.activeElement !== el && tries > 0) requestAnimationFrame(() => focusWhenShown(el, tries - 1));
  };

  const LIST_EDGE = 12;
  const LIST_MIN_ROOM = 160;
  const LIST_MAX_HEIGHT = 288;

  const blankEditor = () => ({
    mode: 'create',
    id: null,
    columnID: null,
    column: '',
    title: '',
    description: '',
    dueDate: '',
    assignees: [],
    labels: [],
    link: null,
    tab: 'write',
    descDirty: false,
    previewHTML: '',
    error: '',
    notice: '',
    busy: false,
    confirm: '',
    card: { assignees: [], labels: [] },
  });

  Alpine.data('kanbanBoard', () => ({
    dragged: null,
    opener: null,
    editor: blankEditor(),
    // Outlives the editor object: a request started before the dialog was reopened still counts.
    inflight: false,
    // The card was changed server-side while the dialog stayed open, so the board behind it is stale.
    changed: false,

    get canWrite() {
      return this.$root.dataset.canWrite === 'true';
    },

    get heading() {
      if (this.editor.mode === 'create') return 'New card in ' + this.editor.column;
      return (this.canWrite ? 'Edit card in ' : 'Card in ') + this.editor.column;
    },

    get titleHint() {
      if (!this.editor.link) return 'Required, unless you link an issue or pull request.';
      if (!this.editor.title.trim()) {
        return 'Without a title the card shows the linked item, and the other details are not saved.';
      }
      return '';
    },

    get linkURL() {
      const link = this.editor.link;
      if (!link) return null;
      const { owner, repoName } = this.$root.dataset;
      const kind = link.kind === 'pull' ? 'pulls' : 'issues';
      return `/${encodeURIComponent(owner)}/${encodeURIComponent(repoName)}/${kind}/${Number(link.number)}`;
    },

    // A titled card is required everywhere except a new card that only links an issue or PR.
    get canSubmit() {
      const e = this.editor;
      return !e.busy && !this.inflight && !e.confirm && (e.title.trim() !== '' || (e.mode === 'create' && e.link !== null));
    },

    // A card keeps an assignee who left the repo; the dropdown no longer offers them, so the
    // assignee dropdown lists them separately and they stay removable.
    get staleAssignees() {
      const listed = new Set(
        Array.from(this.$root.querySelectorAll('[data-card-people] [role=option]:not([data-stale])'), (o) => o.dataset.value),
      );
      return this.editor.card.assignees.filter((a) => !listed.has(String(a.id)));
    },

    onCardClick(e) {
      const card = e.target.closest('[data-card-open]');
      if (card) this.openEdit(card);
    },

    onCardKey(e) {
      if ((e.key === 'Enter' || e.key === ' ') && e.target.matches('[data-card-open]')) {
        e.preventDefault();
        this.openEdit(e.target);
      }
    },

    openCreate(button) {
      if (!this.canWrite) return;
      this.show(button, {
        ...blankEditor(),
        columnID: Number(button.dataset.addCard),
        column: button.dataset.columnName,
      });
    },

    openEdit(li) {
      const c = JSON.parse(li.dataset.cardJson);
      const tpl = document.getElementById('desc-' + c.id);
      this.show(li, {
        ...blankEditor(),
        mode: 'edit',
        id: c.id,
        column: c.column,
        title: c.title,
        description: c.description,
        dueDate: c.due_date,
        assignees: c.assignees.map((a) => String(a.id)),
        labels: c.labels.map((l) => String(l.id)),
        link: c.link_kind
          ? { kind: c.link_kind, id: c.link_id, number: c.link_number, title: c.link_title, state: c.link_state }
          : null,
        tab: this.canWrite ? 'write' : 'preview',
        previewHTML: tpl ? tpl.innerHTML.trim() : '',
        card: c,
      });
    },

    show(opener, state) {
      this.editor = state;
      this.opener = opener;
      const dialog = this.$refs.cardDialog;
      if (!dialog.open) dialog.showModal();
      this.$nextTick(() => {
        const title = this.$refs.cardTitle;
        (title && !title.disabled ? title : this.$refs.cardClose).focus();
      });
    },

    close() {
      if (this.editor.busy) return;
      this.$refs.cardDialog.close();
    },

    // Escape (keydown, or the cancel event where keydown is not the trigger) backs out of the
    // delete/convert confirmation first, and never closes the dialog mid-request.
    onEscape(e) {
      if (this.editor.busy) {
        e.preventDefault();
      } else if (this.editor.confirm) {
        e.preventDefault();
        this.cancelConfirm();
      }
    },

    onDialogClosed() {
      if (this.changed) {
        window.location.reload();
        return;
      }
      if (this.opener && this.opener.isConnected) this.opener.focus();
      this.opener = null;
    },

    pickLink(link) {
      this.editor.link = link;
      this.$nextTick(() => focusWhenShown(this.$refs.linkClear));
    },

    clearLink() {
      this.editor.link = null;
      this.$nextTick(() => focusWhenShown(document.getElementById('card-link-input')));
    },

    body() {
      const e = this.editor;
      const body = {
        title: e.title.trim(),
        description: e.description,
        due_date: e.dueDate || '',
        assignee_ids: e.assignees.map(Number),
        label_ids: e.labels.map(Number),
      };
      if (e.link) body[e.link.kind === 'pull' ? 'pull_id' : 'issue_id'] = e.link.id;
      return body;
    },

    // afterSteps keeps the dialog open instead of reloading the page.
    async run(steps, afterSteps) {
      if (this.editor.busy || this.inflight) return;
      const editor = this.editor;
      this.inflight = true;
      editor.busy = true;
      editor.error = '';
      editor.notice = '';
      try {
        for (const step of steps) await step();
        if (!afterSteps) {
          window.location.reload();
          return;
        }
        afterSteps();
        editor.busy = false;
        this.inflight = false;
      } catch (err) {
        // A reopened dialog has a fresh editor; the old request's failure is not its error.
        if (this.editor === editor) {
          editor.error = err.message;
          editor.busy = false;
        }
        this.inflight = false;
      }
    },

    submit() {
      if (!this.canWrite || !this.canSubmit) return;
      const e = this.editor;
      if (e.mode === 'edit') {
        const url = projectURL(this.$root, `/cards/${e.id}/details`);
        return this.run([() => send('PATCH', url, this.body())]);
      }
      // A title-less card is a plain linked card, whose face shows only the linked item.
      const payload = e.title.trim()
        ? this.body()
        : { [e.link.kind === 'pull' ? 'pull_id' : 'issue_id']: e.link.id };
      return this.run([() => send('POST', projectURL(this.$root, '/cards'), { column_id: e.columnID, ...payload })]);
    },

    // ask swaps the footer for an inline confirmation of a convert or delete.
    ask(action) {
      if (!this.canWrite) return;
      this.editor.error = '';
      this.editor.notice = '';
      this.editor.confirm = action;
      this.$nextTick(() => focusWhenShown(action === 'convert' ? this.$refs.confirmConvert : this.$refs.confirmDelete));
    },

    cancelConfirm() {
      const action = this.editor.confirm;
      this.editor.confirm = '';
      this.$nextTick(() => focusWhenShown(action === 'convert' ? this.$refs.convertButton : this.$refs.deleteButton));
    },

    // Convert saves the dialog first so the new issue gets what the user sees, then keeps the
    // dialog open on the new link; the page behind it reloads when the dialog closes.
    convertCard() {
      if (!this.canWrite || this.editor.confirm !== 'convert') return;
      const editor = this.editor;
      const base = projectURL(this.$root, `/cards/${editor.id}`);
      let card = null;
      return this.run(
        [
          async () => {
            await send('PATCH', base + '/details', this.body());
            this.changed = true;
          },
          async () => {
            const r = await send('POST', base + '/convert');
            this.changed = true;
            card = await r.json().catch(() => null);
          },
        ],
        () => this.showConverted(editor, card),
      );
    },

    showConverted(editor, card) {
      // The issue exists but its details are unreadable: a reload shows the card as linked.
      if (!card || !card.issue_id) {
        window.location.reload();
        return;
      }
      // body() sends editor.link, so the next Save keeps the link convert just made.
      editor.link = { kind: 'issue', id: card.issue_id, number: card.issue_number, title: card.issue_title, state: card.issue_state };
      editor.confirm = '';
      editor.notice = `Issue #${card.issue_number} created and linked`;
      this.$nextTick(() => focusWhenShown(this.$refs.linkAnchor));
    },

    deleteCard() {
      if (!this.canWrite || this.editor.confirm !== 'delete') return;
      return this.run([() => send('DELETE', projectURL(this.$root, `/cards/${this.editor.id}`))]);
    },

    onDragStart(e) {
      const t = e.target.closest('[data-drag="kanban-card"]');
      if (!t) return;
      this.dragged = t;
      if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
    },

    onDragOver(e) {
      if (!this.dragged) return;
      const col = e.target.closest('[data-drop="kanban-column"]');
      if (col) e.preventDefault();
    },

    async onDrop(e) {
      const col = e.target.closest('[data-drop="kanban-column"]');
      if (!col || !this.dragged) return;
      e.preventDefault();

      // Capture the dragged element locally so a concurrent drag started
      // during the awaited fetch doesn't get its state clobbered by finally.
      const dragged = this.dragged;
      const cardID = dragged.dataset.cardId;
      const columnID = col.dataset.columnId;
      const list = col.querySelector('[data-cards]') || col;
      list.appendChild(dragged);
      const position = Array.from(list.querySelectorAll('[data-drag="kanban-card"]')).indexOf(dragged);

      try {
        await send('PATCH', projectURL(this.$root, `/cards/${cardID}`), { column_id: Number(columnID), position });
      } catch (err) {
        console.error('kanban move failed:', err);
        window.alert('Could not move card (' + err.message + '). Reloading.');
        window.location.reload();
      } finally {
        if (this.dragged === dragged) this.dragged = null;
      }
    },
  }));

  Alpine.data('columnDialog', () => ({
    name: '',
    error: '',
    busy: false,

    get blank() {
      return this.name.trim() === '';
    },

    init() {
      this.$el.closest('dialog').addEventListener('close', () => {
        this.name = '';
        this.error = '';
        this.busy = false;
      });
    },

    async submit() {
      if (this.busy || this.blank) return;
      this.busy = true;
      this.error = '';
      try {
        await send('POST', projectURL(this.$el, '/columns'), { name: this.name.trim() });
        window.location.reload();
      } catch (err) {
        this.error = err.message;
        this.busy = false;
      }
    },
  }));

  // linkPicker searches the repo's issues and PRs by title fragment or #number and
  // dispatches card-link-picked with the pick.
  Alpine.data('linkPicker', () => ({
    text: '',
    results: [],
    active: 0,
    pending: false,
    searched: false,
    error: '',
    listStyle: '',
    seq: 0,
    unbind: null,

    get query() {
      const q = this.text.trim().replace(/^#/, '');
      return q ? q : null;
    },

    get showList() {
      return this.query !== null && (this.pending || this.searched || this.results.length > 0);
    },

    init() {
      const follow = () => {
        if (this.showList) this.place();
      };
      const onClose = () => this.reset();
      const dialog = this.$el.closest('dialog');
      window.addEventListener('scroll', follow, true);
      window.addEventListener('resize', follow);
      if (dialog) dialog.addEventListener('close', onClose);
      this.unbind = () => {
        window.removeEventListener('scroll', follow, true);
        window.removeEventListener('resize', follow);
        if (dialog) dialog.removeEventListener('close', onClose);
      };
    },

    destroy() {
      if (this.unbind) this.unbind();
    },

    onFocusOut(e) {
      if (!this.$el.contains(e.relatedTarget)) this.reset();
    },

    // The list opens below the input unless that leaves too little room and more fits above;
    // it scrolls within the room it has, so it is never cut off by the viewport.
    place() {
      const r = this.$refs.input.getBoundingClientRect();
      const below = window.innerHeight - r.bottom - LIST_EDGE;
      const above = r.top - LIST_EDGE;
      const down = below >= LIST_MIN_ROOM || below >= above;
      const height = Math.min(LIST_MAX_HEIGHT, down ? below : above);
      // Anchoring to the input's top edge keeps a short list next to the input when it opens above.
      const anchor = down ? `top:${r.bottom + 4}px` : `bottom:${window.innerHeight - r.top + 4}px`;
      this.listStyle = `position:fixed;left:${r.left}px;${anchor};width:${r.width}px;max-height:${height}px`;
    },

    reset() {
      this.seq++;
      this.text = '';
      this.results = [];
      this.pending = false;
      this.searched = false;
      this.error = '';
    },

    async search() {
      const q = this.query;
      const seq = ++this.seq;
      if (q === null) {
        this.results = [];
        this.pending = false;
        this.searched = false;
        this.error = '';
        return;
      }
      this.pending = true;
      this.place();
      try {
        const r = await send('GET', projectURL(this.$el, '/card-targets?q=' + encodeURIComponent(q)));
        const found = await r.json();
        if (seq !== this.seq) return;
        this.results = found || [];
        this.error = '';
      } catch (err) {
        if (seq !== this.seq) return;
        this.results = [];
        this.error = 'Search failed: ' + err.message;
      }
      this.active = 0;
      this.pending = false;
      this.searched = true;
      this.place();
    },

    move(step) {
      if (!this.results.length) return;
      this.active = (this.active + step + this.results.length) % this.results.length;
    },

    // Enter never submits the dialog while a search is pending or empty.
    enterPick() {
      if (!this.pending && this.results.length) this.pick(this.results[this.active]);
    },

    pick(target) {
      this.$dispatch('card-link-picked', {
        kind: target.kind,
        id: target.id,
        number: target.number,
        title: target.title,
        state: target.state,
      });
      this.reset();
    },
  }));
});
