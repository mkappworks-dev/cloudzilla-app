// kanban.js — Alpine-driven drag state, htmx.ajax-driven network calls.
//
// The board root element must declare:
//   x-data="kanbanBoard"
//   data-owner="<owner>" data-repo-name="<repo>" data-project-id="<id>"
//
// Each card carries data-drag="kanban-card" and data-card-id="<id>".
// Each column declares data-drop="kanban-column" and data-column-id="<id>",
// and contains a [data-cards] list of card elements.
document.addEventListener('alpine:init', () => {
  Alpine.data('kanbanBoard', () => ({
    dragged: null,

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

      const cardID = this.dragged.dataset.cardId;
      const columnID = col.dataset.columnId;
      const list = col.querySelector('[data-cards]') || col;
      list.appendChild(this.dragged); // optimistic insert at end of column
      const position = Array.from(list.querySelectorAll('[data-drag="kanban-card"]')).indexOf(this.dragged);

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
        if (!r.ok) throw new Error('HTTP ' + r.status);
      } catch (err) {
        // Server is the source of truth — revert by reloading.
        window.location.reload();
      } finally {
        this.dragged = null;
      }
    },
  }));
});
