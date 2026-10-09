// kanban.js — Alpine components for the project board.
//
// The board root declares x-data="kanbanBoard" with data-owner, data-repo-name,
// data-project-id and data-can-write. Cards carry data-drag="kanban-card" and
// data-card-id; columns carry data-drop="kanban-column", data-column-id and a
// [data-cards] list. Cards that open the side panel carry data-card-panel and
// data-card-json; their rendered description sits in <template id="desc-{id}">.
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

  // targetPicker searches the repo's issues and PRs for the text that query()
  // extracts (null when the text is not a search); the component supplies onPick.
  const targetPicker = (query) => ({
    text: '',
    results: [],
    active: 0,
    pending: false,
    searched: false,
    error: '',
    listStyle: '',
    seq: 0,

    get query() {
      return query(this.text);
    },

    get showList() {
      return this.query !== null && (this.pending || this.searched || this.results.length > 0);
    },

    init() {
      const follow = () => {
        if (this.showList) this.place();
      };
      window.addEventListener('scroll', follow, true);
      window.addEventListener('resize', follow);
    },

    place() {
      const r = this.$refs.input.getBoundingClientRect();
      this.listStyle = `position:fixed;left:${r.left}px;top:${r.bottom + 4}px;width:${r.width}px`;
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

    // Enter never falls through to plain text while a search is pending or empty.
    enterPick() {
      if (!this.pending && this.results.length) this.pick(this.results[this.active]);
    },

    pick(target) {
      return this.onPick(target);
    },
  });

  const withPicker = (query, extra) =>
    Object.defineProperties(targetPicker(query), Object.getOwnPropertyDescriptors(extra));

  Alpine.data('kanbanBoard', () => ({
    dragged: null,
    opener: null,
    panel: {
      open: false,
      id: null,
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
      busy: false,
      card: { assignees: [], labels: [] },
    },

    get canWrite() {
      return this.$root.dataset.canWrite === 'true';
    },

    // A card keeps an assignee who left the repo; the picker no longer lists them, so the
    // panel shows them separately and they stay untickable.
    get staleAssignees() {
      const listed = new Set(Array.from(this.$root.querySelectorAll('[data-person]'), (i) => i.value));
      return this.panel.card.assignees.filter((a) => !listed.has(String(a.id)));
    },

    onCardClick(e) {
      const card = e.target.closest('[data-card-panel]');
      if (card) this.openPanel(card);
    },

    onCardKey(e) {
      if ((e.key === 'Enter' || e.key === ' ') && e.target.matches('[data-card-panel]')) {
        e.preventDefault();
        this.openPanel(e.target);
      }
    },

    openPanel(li) {
      const c = JSON.parse(li.dataset.cardJson);
      const tpl = document.getElementById('desc-' + c.id);
      Object.assign(this.panel, {
        open: true,
        id: c.id,
        column: c.column,
        title: c.title,
        description: c.description,
        dueDate: c.due_date,
        assignees: c.assignees.map((a) => String(a.id)),
        labels: c.labels.map((l) => String(l.id)),
        link: c.link_kind ? { kind: c.link_kind, id: c.link_id, number: c.link_number, title: c.link_title } : null,
        tab: this.canWrite ? 'write' : 'preview',
        descDirty: false,
        previewHTML: tpl ? tpl.innerHTML.trim() : '',
        error: '',
        busy: false,
        card: c,
      });
      this.opener = li;
      this.$nextTick(() => {
        const title = this.$refs.panelTitle;
        (title && !title.disabled ? title : this.$refs.panelClose).focus();
      });
    },

    closePanel() {
      if (!this.panel.open) return;
      this.panel.open = false;
      if (this.opener) this.opener.focus();
    },

    panelBody() {
      const p = this.panel;
      const body = {
        title: p.title.trim(),
        description: p.description,
        due_date: p.dueDate || '',
        assignee_ids: p.assignees.map(Number),
        label_ids: p.labels.map(Number),
      };
      if (p.link) body[p.link.kind === 'pull' ? 'pull_id' : 'issue_id'] = p.link.id;
      return body;
    },

    async panelRequest(steps) {
      this.panel.busy = true;
      this.panel.error = '';
      try {
        for (const step of steps) await step();
        window.location.reload();
      } catch (err) {
        this.panel.error = err.message;
        this.panel.busy = false;
      }
    },

    savePanel() {
      const url = projectURL(this.$root, `/cards/${this.panel.id}/details`);
      return this.panelRequest([() => send('PATCH', url, this.panelBody())]);
    },

    // Convert saves the panel first so the new issue gets what the user sees.
    convertPanel() {
      if (!window.confirm('Convert this card into an issue? The card will link to the new issue.')) return;
      const base = projectURL(this.$root, `/cards/${this.panel.id}`);
      return this.panelRequest([
        () => send('PATCH', base + '/details', this.panelBody()),
        () => send('POST', base + '/convert'),
      ]);
    },

    deletePanel() {
      if (!window.confirm('Delete this card?')) return;
      const url = projectURL(this.$root, `/cards/${this.panel.id}`);
      return this.panelRequest([() => send('DELETE', url)]);
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

  Alpine.data('cardComposer', (columnID) =>
    withPicker((text) => (text.startsWith('#') && !text.includes('\n') ? text.slice(1) : null), {
      open: false,
      busy: false,
      createError: '',

      show() {
        this.open = true;
        this.$nextTick(() => this.$refs.input.focus());
      },

      close() {
        this.open = false;
        this.createError = '';
        this.reset();
      },

      onEnter(e) {
        e.preventDefault();
        if (this.query !== null) this.enterPick();
        else this.submit();
      },

      onPick(target) {
        return this.create(target.kind === 'pull' ? { pull_id: target.id } : { issue_id: target.id });
      },

      submit() {
        const title = this.text.replace(/\s+/g, ' ').trim();
        if (title && this.query === null) return this.create({ title });
      },

      async create(payload) {
        this.busy = true;
        this.createError = '';
        try {
          await send('POST', projectURL(this.$el, '/cards'), { column_id: columnID, ...payload });
          window.location.reload();
        } catch (err) {
          this.createError = err.message;
          this.busy = false;
        }
      },
    }),
  );

  Alpine.data('linkPicker', () =>
    withPicker((text) => (text.trim() ? text.trim().replace(/^#/, '') : null), {
      onPick(target) {
        this.$dispatch('card-link-picked', { kind: target.kind, id: target.id, number: target.number, title: target.title });
        this.reset();
      },
    }),
  );
});
