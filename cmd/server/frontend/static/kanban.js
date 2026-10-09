// kanban.js — Alpine-driven drag state, fetch-driven network calls.
//
// The board root element must declare:
//   x-data="kanbanBoard"
//   data-owner="<owner>" data-repo-name="<repo>" data-project-id="<id>"
//
// Each card carries data-drag="kanban-card" and data-card-id="<id>".
// Each column declares data-drop="kanban-column" and data-column-id="<id>",
// and contains a [data-cards] list of card elements.
//
// Note cards carry data-card-kind="note" and data-note="<full text>"; clicking
// one opens the #note-dialog declared inside the board root. A composer
// (cardComposer) sits under each column for writers.
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

  Alpine.data('kanbanBoard', () => ({
    dragged: null,
    noteCard: null,
    noteText: '',
    noteError: '',
    noteBusy: false,

    onCardClick(e) {
      const card = e.target.closest('[data-card-kind="note"]');
      if (card) this.openNote(card);
    },

    onCardKey(e) {
      if (e.target.matches('[data-card-kind="note"]')) this.openNote(e.target);
    },

    openNote(card) {
      this.noteCard = card.dataset.cardId;
      this.noteText = card.dataset.note;
      this.noteError = '';
      document.getElementById('note-dialog').showModal();
    },

    async saveNote() {
      this.noteBusy = true;
      try {
        await send('PATCH', projectURL(this.$el, `/cards/${this.noteCard}/note`), { note: this.noteText });
        window.location.reload();
      } catch (err) {
        this.noteError = err.message;
        this.noteBusy = false;
      }
    },

    async deleteNote() {
      if (!window.confirm('Delete this note?')) return;
      try {
        await send('DELETE', projectURL(this.$el, `/cards/${this.noteCard}`));
        window.location.reload();
      } catch (err) {
        this.noteError = err.message;
      }
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

      const root = this.$root;
      const owner = root.dataset.owner;
      const repoName = root.dataset.repoName;
      const projectID = root.dataset.projectId;
      const url = `/api/repos/${owner}/${repoName}/projects/${projectID}/cards/${cardID}`;
      const csrf = (document.cookie.match(/csrf_token=([^;]+)/) || [])[1] || '';

      try {
        const r = await fetch(url, {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
          body: JSON.stringify({ column_id: Number(columnID), position }),
        });
        if (!r.ok) {
          const body = await r.text().catch((readErr) => {
            console.warn('kanban: response body read failed', readErr);
            return '';
          });
          throw new Error('HTTP ' + r.status + (body ? ': ' + body.slice(0, 200) : ''));
        }
      } catch (err) {
        console.error('kanban move failed:', err);
        const msg = (err && err.message) ? err.message : 'unknown error';
        window.alert('Could not move card (' + msg + '). Reloading.');
        window.location.reload();
      } finally {
        if (this.dragged === dragged) this.dragged = null;
      }
    },
  }));

  Alpine.data('cardComposer', (columnID) => ({
    open: false,
    text: '',
    results: [],
    active: 0,
    error: '',
    busy: false,
    seq: 0,

    get isQuery() {
      return this.text.startsWith('#') && !this.text.includes('\n');
    },

    show() {
      this.open = true;
      this.$nextTick(() => this.$refs.input.focus());
    },

    close() {
      this.open = false;
      this.text = '';
      this.results = [];
      this.error = '';
    },

    async search() {
      if (!this.isQuery) {
        this.results = [];
        return;
      }
      const seq = ++this.seq;
      try {
        const r = await send('GET', projectURL(this.$el, '/card-targets?q=' + encodeURIComponent(this.text.slice(1))));
        const found = await r.json();
        if (seq === this.seq) {
          this.results = found;
          this.active = 0;
        }
      } catch (err) {
        if (seq === this.seq) this.results = [];
      }
    },

    move(step) {
      if (!this.results.length) return;
      this.active = (this.active + step + this.results.length) % this.results.length;
    },

    onEnter(e) {
      if (e.shiftKey) return;
      e.preventDefault();
      if (this.isQuery && this.results.length) this.pick(this.results[this.active]);
      else this.submit();
    },

    pick(target) {
      return this.create(target.kind === 'pull' ? { pull_id: target.id } : { issue_id: target.id });
    },

    submit() {
      const note = this.text.trim();
      if (note) return this.create({ note });
    },

    async create(payload) {
      this.busy = true;
      this.error = '';
      try {
        await send('POST', projectURL(this.$el, '/cards'), { column_id: columnID, ...payload });
        window.location.reload();
      } catch (err) {
        this.error = err.message;
        this.busy = false;
      }
    },
  }));
});
