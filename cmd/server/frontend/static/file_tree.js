// Keeps the file tree's open folders in a cookie that the server reads to
// render them open on the next page (internal/handler/file_tree_handler.go).
document.addEventListener('alpine:init', () => {
  const COOKIE = 'cz_tree_open';
  const MAX_FOLDERS = 50; // the server ignores entries past maxOpenTreeFolders

  Alpine.data('fileTree', () => ({
    filter: '',
    openFolders: [],
    repoPath: '/',

    init() {
      this.repoPath = this.$el.dataset.repoPath;
      const match = document.cookie.match(/(?:^|; )cz_tree_open=([^;]*)/);
      try {
        const saved = match ? JSON.parse(decodeURIComponent(match[1])) : [];
        this.openFolders = Array.isArray(saved) ? saved.filter((p) => typeof p === 'string') : [];
      } catch {
        this.openFolders = [];
      }
    },

    remember(path) {
      this.openFolders = this.openFolders.filter((p) => p !== path).concat(path).slice(-MAX_FOLDERS);
      this.save();
    },

    forget(path) {
      this.openFolders = this.openFolders.filter((p) => p !== path && !p.startsWith(path + '/'));
      this.save();
    },

    collapseAll() {
      window.dispatchEvent(new CustomEvent('cz-tree-collapse-all'));
      this.openFolders = [];
      this.save();
    },

    save() {
      document.cookie = COOKIE + '=' + encodeURIComponent(JSON.stringify(this.openFolders)) +
        '; path=' + this.repoPath + '; SameSite=Lax';
    },
  }));
});
