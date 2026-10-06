// Keeps the file tree's open folders in a cookie that the server reads to
// render them open on the next page (internal/handler/file_tree_handler.go).
document.addEventListener('alpine:init', () => {
  const COOKIE = 'cz_tree_open';
  const MAX_FOLDERS = 50; // the server ignores entries past maxOpenTreeFolders
  const MAX_ENCODED = 3800; // browsers drop a cookie past 4 KB, which would freeze the tree

  // Every write starts from the live cookie: a second tab or a page restored
  // from the back/forward cache holds a stale list and would overwrite folders
  // opened elsewhere.
  function read() {
    const match = document.cookie.match(new RegExp('(?:^|; )' + COOKIE + '=([^;]*)'));
    try {
      const saved = match ? JSON.parse(decodeURIComponent(match[1])) : [];
      return Array.isArray(saved) ? saved.filter((p) => typeof p === 'string') : [];
    } catch {
      return [];
    }
  }

  // cz_tree_hidden is site-wide (path=/), unlike the per-repo open folders:
  // hiding the tree is a layout preference, not a view of one repo.
  Alpine.data('fileTreePanel', (hidden) => ({
    treeHidden: hidden,

    hideTree() {
      this.treeHidden = true;
      document.cookie = 'cz_tree_hidden=1; path=/; SameSite=Lax';
    },

    showTree() {
      this.treeHidden = false;
      document.cookie = 'cz_tree_hidden=; path=/; max-age=0; SameSite=Lax';
    },
  }));

  Alpine.data('fileTree', () => ({
    filter: '',
    repoPath: '/',

    init() {
      this.repoPath = this.$el.dataset.repoPath;
    },

    remember(path) {
      this.save(read().filter((p) => p !== path).concat(path).slice(-MAX_FOLDERS));
    },

    forget(path) {
      this.save(read().filter((p) => p !== path && !p.startsWith(path + '/')));
    },

    collapseAll() {
      window.dispatchEvent(new CustomEvent('cz-tree-collapse-all'));
      this.save([]);
    },

    save(list) {
      while (list.length > 0 && encodeURIComponent(JSON.stringify(list)).length > MAX_ENCODED) {
        list = list.slice(1);
      }
      document.cookie = COOKIE + '=' + encodeURIComponent(JSON.stringify(list)) +
        '; path=' + this.repoPath + '; SameSite=Lax';
    },
  }));
});
