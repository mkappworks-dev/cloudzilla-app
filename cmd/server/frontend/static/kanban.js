// kanban.js — Alpine components for the project board.
//
// The board root declares x-data="kanbanBoard" with data-owner, data-repo-name,
// data-project-id and data-can-write. Cards carry data-drag="kanban-card" and
// data-card-id; columns carry data-drop="kanban-column", data-column-id and a
// [data-cards] list. Cards that open the card dialog carry data-card-open and
// data-card-json. Each column's "+ Add item" button carries data-add-card and
// data-column-name.
//
// The dialog creates a card in one POST, or shows a card whose fields a writer edits and
// saves one at a time through PATCH .../fields.
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
  const SAVED_STATUS_MS = 2000;
  const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;
  const DESCRIPTION_ID = 'card-description';

  const FIELDS = ['title', 'description', 'assignees', 'labels', 'due', 'link'];
  const blankField = () => ({ editing: false, saving: false, status: '', error: '', timer: null });

  const blankSaved = () => ({
    title: '',
    description: '',
    descriptionHTML: '',
    dueDate: '',
    assignees: [],
    labels: [],
    link: null,
  });

  const blankEditor = () => ({
    mode: 'create',
    id: null,
    columnID: null,
    column: '',
    // Drafts: the create form's fields, and in edit mode what a field shows while it is edited.
    title: '',
    dueDate: '',
    assignees: [],
    labels: [],
    link: null,
    saved: blankSaved(),
    fields: Object.fromEntries(FIELDS.map((f) => [f, blankField()])),
    lastEdited: '',
    // Close was asked for while a field saved; the dialog closes once every save has succeeded.
    closeWhenSaved: false,
    error: '',
    notice: '',
    busy: false,
    confirm: '',
  });

  const linkFromJSON = (kind, id, number, title, state) => (kind ? { kind, id, number, title, state } : null);

  // savedFromResponse reads the PATCH .../fields reply, which describes the whole card.
  const savedFromResponse = (card) => ({
    title: card.title,
    description: card.note,
    descriptionHTML: card.description_html || '',
    dueDate: card.due_date ? card.due_date.slice(0, 10) : '',
    assignees: card.assignees || [],
    labels: (card.labels || []).map((l) => ({ id: l.id, name: l.name, color: HEX_COLOR.test(l.color) ? l.color : '' })),
    link: card.issue_id
      ? linkFromJSON('issue', card.issue_id, card.issue_number, card.issue_title, card.issue_state)
      : linkFromJSON(card.pull_id ? 'pull' : '', card.pull_id, card.pull_number, card.pull_title, card.pull_state),
  });

  const sameSet = (a, b) => a.length === b.length && a.every((v) => b.includes(v));

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
      return this.editor.saved.title;
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

    get canSubmit() {
      const e = this.editor;
      return e.mode === 'create' && !e.busy && !this.inflight && (e.title.trim() !== '' || e.link !== null);
    },

    get anySaving() {
      return FIELDS.some((f) => this.editor.fields[f].saving);
    },

    // The dialog stays open while a field saves, so its error has somewhere to show.
    get closeBlocked() {
      return this.editor.busy || this.anySaving;
    },

    get anyEditing() {
      return FIELDS.some((f) => this.editor.fields[f].editing);
    },

    // A card keeps an assignee who left the repo; the dropdown no longer offers them, so the
    // assignee dropdown lists them separately and they stay removable.
    get staleAssignees() {
      const listed = new Set(
        Array.from(this.$root.querySelectorAll('[data-card-people] [role=option]:not([data-stale])'), (o) => o.dataset.value),
      );
      return this.editor.saved.assignees.filter((a) => !listed.has(String(a.id)));
    },

    fieldStatus(name) {
      const f = this.editor.fields[name];
      if (f.error) return f.error;
      if (f.status === 'saving') return 'Saving…';
      if (f.status === 'saved') return 'Saved';
      return '';
    },

    dueLabel(iso) {
      const d = iso && window.CzDate ? CzDate.parseISODate(iso) : null;
      return d ? CzDate.formatShort(d) : iso;
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
      this.setDescription('');
      this.show(button, {
        ...blankEditor(),
        columnID: Number(button.dataset.addCard),
        column: button.dataset.columnName,
      });
    },

    openEdit(li) {
      const c = JSON.parse(li.dataset.cardJson);
      const editor = {
        ...blankEditor(),
        mode: 'edit',
        id: c.id,
        column: c.column,
        saved: {
          title: c.title,
          description: c.description,
          descriptionHTML: c.description_html,
          dueDate: c.due_date,
          assignees: c.assignees,
          labels: c.labels,
          link: linkFromJSON(c.link_kind, c.link_id, c.link_number, c.link_title, c.link_state),
        },
      };
      for (const f of FIELDS) this.resetDraft(editor, f);
      this.show(li, editor);
    },

    show(opener, state) {
      this.editor = state;
      this.opener = opener;
      const dialog = this.$refs.cardDialog;
      if (!dialog.open) dialog.showModal();
      this.$nextTick(() => {
        if (state.mode === 'create') focusWhenShown(this.$refs.cardTitle);
        else focusWhenShown(this.canWrite ? this.$refs.titleEditButton : this.$refs.cardClose);
      });
    },

    // close saves a dropdown left open first, as closing its popover would, and then waits for
    // that save: the dialog closes when it succeeds and stays open on its error.
    close() {
      if (this.closeBlocked) return;
      this.commitOpenDropdowns();
      if (this.anySaving) {
        this.editor.closeWhenSaved = true;
        return;
      }
      this.$refs.cardDialog.close();
    },

    // commitOpenDropdowns closes any open dropdown, whose multiselect-closed starts its save
    // synchronously; a click outside it would close it only after this click's own handler.
    commitOpenDropdowns() {
      for (const el of this.$refs.cardDialog.querySelectorAll('[x-data="multiSelect"]')) {
        const dropdown = Alpine.$data(el);
        if (dropdown.open) dropdown.close(false);
      }
    },

    // Escape (keydown, or the cancel event where keydown is not the trigger) backs out of a field
    // edited in place, then the delete/convert confirmation, and never closes the dialog
    // mid-request. Popovers and the picker list stop their own Escape before it gets here.
    onEscape(e) {
      if (this.closeBlocked) {
        e.preventDefault();
        return;
      }
      const field = this.editingField(e.target);
      if (field) {
        e.preventDefault();
        this.cancelField(field);
      } else if (this.editor.confirm) {
        e.preventDefault();
        this.cancelConfirm();
      }
    },

    // editingField is the field Escape cancels: the one holding focus, else the last one opened.
    editingField(target) {
      const open = FIELDS.filter((f) => this.editor.fields[f].editing);
      if (!open.length) return '';
      const el = target && target.closest ? target.closest('[data-card-field]') : null;
      if (el && open.includes(el.dataset.cardField)) return el.dataset.cardField;
      return open.includes(this.editor.lastEdited) ? this.editor.lastEdited : open[0];
    },

    // Closing drops a field edited in place without saving it.
    onDialogClosed() {
      if (this.changed) {
        window.location.reload();
        return;
      }
      if (this.opener && this.opener.isConnected) this.opener.focus();
      this.opener = null;
    },

    descriptionTextarea() {
      return document.getElementById(DESCRIPTION_ID);
    },

    // setDescription fills the shared markdown editor and shows its Write tab with an empty preview.
    setDescription(text) {
      const ta = this.descriptionTextarea();
      if (!ta) return;
      ta.value = text;
      const ed = ta.closest('[data-md-editor]');
      const write = ed.querySelector('[data-md-tab="write"]');
      if (write.getAttribute('aria-selected') !== 'true') write.click();
      const preview = document.getElementById(DESCRIPTION_ID + '-preview');
      if (preview) preview.replaceChildren();
    },

    resetDraft(editor, name) {
      const s = editor.saved;
      if (name === 'title') editor.title = s.title;
      else if (name === 'assignees') editor.assignees = s.assignees.map((a) => String(a.id));
      else if (name === 'labels') editor.labels = s.labels.map((l) => String(l.id));
      else if (name === 'due') editor.dueDate = s.dueDate;
      else if (name === 'link') editor.link = s.link;
    },

    editButton(name) {
      return this.$refs[name + 'EditButton'];
    },

    editField(name) {
      const e = this.editor;
      if (!this.canWrite || e.mode !== 'edit' || e.busy || e.fields[name].saving) return;
      const f = e.fields[name];
      f.editing = true;
      f.error = '';
      e.lastEdited = name;
      this.resetDraft(e, name);
      if (name === 'title') this.$nextTick(() => focusWhenShown(this.$refs.titleEdit));
      if (name === 'description') {
        this.setDescription(e.saved.description);
        this.$nextTick(() => focusWhenShown(this.descriptionTextarea()));
      }
      if (name === 'link') this.$nextTick(() => focusWhenShown(document.getElementById('card-link-input')));
    },

    cancelField(name) {
      const e = this.editor;
      const f = e.fields[name];
      if (f.saving) return;
      f.editing = false;
      f.error = '';
      this.resetDraft(e, name);
      this.$nextTick(() => focusWhenShown(this.editButton(name)));
    },

    // finishEdit closes a field edited in place once its save is done.
    finishEdit(name) {
      this.editor.fields[name].editing = false;
      this.$nextTick(() => focusWhenShown(this.editButton(name)));
    },

    saveTitle() {
      const e = this.editor;
      const title = e.title.trim();
      if (!title) {
        e.fields.title.error = 'A title is required.';
        return;
      }
      if (title === e.saved.title) return this.finishEdit('title');
      this.saveField('title', { title }, () => this.finishEdit('title'));
    },

    saveDescription() {
      const ta = this.descriptionTextarea();
      const text = ta ? ta.value : '';
      if (text === this.editor.saved.description) return this.finishEdit('description');
      this.saveField('description', { description: text }, () => this.finishEdit('description'));
    },

    // commitField saves a dropdown or the due date when the user is done choosing; the value
    // comes with the event because x-model catches up a tick later.
    commitField(name, value) {
      const e = this.editor;
      if (!this.canWrite || e.mode !== 'edit') return;
      const s = e.saved;
      if (name === 'assignees') {
        const ids = value.map(Number);
        if (sameSet(ids, s.assignees.map((a) => a.id))) return;
        this.saveField(name, { assignee_ids: ids });
      } else if (name === 'labels') {
        const ids = value.map(Number);
        if (sameSet(ids, s.labels.map((l) => l.id))) return;
        this.saveField(name, { label_ids: ids });
      } else if (name === 'due') {
        if (value === s.dueDate) return;
        this.saveField(name, { due_date: value || null });
      }
    },

    // saveField sends one field and, on success, takes only that field from the reply: a reply
    // to another field's save that lands later must not roll this one back.
    async saveField(name, body, onSaved) {
      const editor = this.editor;
      const f = editor.fields[name];
      if (f.saving) return;
      if (!this.canWrite || editor.busy) {
        // The choice was not saved, so the control must not keep showing it.
        if (!f.editing) this.resetDraft(editor, name);
        return;
      }
      clearTimeout(f.timer);
      f.saving = true;
      f.status = 'saving';
      f.error = '';
      try {
        const r = await send('PATCH', projectURL(this.$root, `/cards/${editor.id}/fields`), body);
        this.changed = true;
        const card = savedFromResponse(await r.json());
        if (name === 'description') {
          editor.saved.description = card.description;
          editor.saved.descriptionHTML = card.descriptionHTML;
        } else {
          const key = { title: 'title', assignees: 'assignees', labels: 'labels', due: 'dueDate', link: 'link' }[name];
          editor.saved[key] = card[key];
          this.resetDraft(editor, name);
        }
        f.saving = false;
        f.status = 'saved';
        f.timer = setTimeout(() => {
          if (f.status === 'saved') f.status = '';
        }, SAVED_STATUS_MS);
        if (onSaved && this.editor === editor) onSaved();
      } catch (err) {
        f.saving = false;
        f.status = '';
        f.error = err.message;
        // A field edited in place keeps the typed text to retry; the others show the stored value again.
        if (!f.editing) this.resetDraft(editor, name);
        // Stay open on the error.
        editor.closeWhenSaved = false;
      }
      this.closeIfSaved(editor);
    },

    closeIfSaved(editor) {
      if (!editor.closeWhenSaved || FIELDS.some((n) => editor.fields[n].saving)) return;
      editor.closeWhenSaved = false;
      if (this.editor === editor) this.$refs.cardDialog.close();
    },

    pickLink(link) {
      const e = this.editor;
      if (e.mode === 'create') {
        e.link = link;
        this.$nextTick(() => focusWhenShown(this.$refs.linkClear));
        return;
      }
      const body = { issue_id: link.kind === 'issue' ? link.id : null, pull_id: link.kind === 'pull' ? link.id : null };
      e.link = link;
      e.fields.link.editing = false;
      this.saveField('link', body, () => this.$nextTick(() => focusWhenShown(this.$refs.linkAnchor)));
    },

    clearLink() {
      const e = this.editor;
      if (e.mode === 'create') {
        e.link = null;
        this.$nextTick(() => focusWhenShown(document.getElementById('card-link-input')));
        return;
      }
      e.link = null;
      e.fields.link.editing = false;
      this.saveField('link', { issue_id: null, pull_id: null }, () =>
        this.$nextTick(() => focusWhenShown(document.getElementById('card-link-input'))),
      );
    },

    body() {
      const e = this.editor;
      const ta = this.descriptionTextarea();
      const body = {
        title: e.title.trim(),
        description: ta ? ta.value : '',
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

    // submit creates a card; in edit mode each field saves on its own.
    submit() {
      if (!this.canWrite || !this.canSubmit) return;
      const e = this.editor;
      // A title-less card is a plain linked card, whose face shows only the linked item.
      const payload = e.title.trim()
        ? this.body()
        : { [e.link.kind === 'pull' ? 'pull_id' : 'issue_id']: e.link.id };
      return this.run([() => send('POST', projectURL(this.$root, '/cards'), { column_id: e.columnID, ...payload })]);
    },

    // ask swaps the footer for an inline confirmation of a convert or delete. A dropdown left
    // open is saved first, and the action waits: it is refused until every field save is done.
    ask(action) {
      if (!this.canWrite) return;
      this.commitOpenDropdowns();
      if (this.actionBlocked(action)) return;
      this.editor.error = '';
      this.editor.notice = '';
      this.editor.confirm = action;
      this.$nextTick(() => focusWhenShown(action === 'convert' ? this.$refs.confirmConvert : this.$refs.confirmDelete));
    },

    // Convert copies the stored card, so it also waits for fields edited in place.
    actionBlocked(action) {
      return this.anySaving || (action === 'convert' && this.anyEditing);
    },

    cancelConfirm() {
      const action = this.editor.confirm;
      this.editor.confirm = '';
      this.$nextTick(() => focusWhenShown(action === 'convert' ? this.$refs.convertButton : this.$refs.deleteButton));
    },

    // Every field is saved on its own and Convert is disabled while one is edited or saving, so
    // the new issue gets the stored card. The dialog stays open on the new link; the page behind
    // it reloads when the dialog closes.
    convertCard() {
      if (!this.canWrite || this.editor.confirm !== 'convert') return;
      this.commitOpenDropdowns();
      if (this.actionBlocked('convert')) return;
      const editor = this.editor;
      let card = null;
      return this.run(
        [
          async () => {
            const r = await send('POST', projectURL(this.$root, `/cards/${editor.id}/convert`));
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
      editor.saved.link = linkFromJSON('issue', card.issue_id, card.issue_number, card.issue_title, card.issue_state);
      editor.link = editor.saved.link;
      editor.confirm = '';
      editor.notice = `Issue #${card.issue_number} created and linked`;
      this.$nextTick(() => focusWhenShown(this.$refs.linkAnchor));
    },

    deleteCard() {
      if (!this.canWrite || this.editor.confirm !== 'delete') return;
      this.commitOpenDropdowns();
      if (this.actionBlocked('delete')) return;
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
