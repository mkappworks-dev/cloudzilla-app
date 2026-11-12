// release-assets.js — the `releaseAssetUpload` Alpine component on the release detail page,
// and window.CzReleaseAssets.upload, which the new-release form reuses.
(() => {
  const csrf = () => (document.cookie.match(/csrf_token=([^;]+)/) || [])[1] || '';

  // Mirrors service.CleanReleaseAssetName so duplicates are caught before the upload starts;
  // the server stays the authority.
  const cleanName = (filename) => filename.split(/[\\/]/).pop().replace(/[^A-Za-z0-9._-]/g, '-');

  // fetch has no upload progress, so this is an XHR. Resolves with the created asset; rejects
  // with an Error whose message is safe to show.
  const upload = (url, file, onProgress) =>
    new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open('POST', url);
      xhr.setRequestHeader('X-CSRF-Token', csrf());
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress(e.loaded / e.total);
      };
      xhr.onload = () => {
        let body = {};
        try {
          body = JSON.parse(xhr.responseText);
        } catch (err) {}
        if (xhr.status >= 200 && xhr.status < 300) resolve(body);
        else reject(new Error(body.error || 'The upload failed (HTTP ' + xhr.status + ').'));
      };
      xhr.onerror = () => reject(new Error('The upload failed. Check your connection and try again.'));
      const form = new FormData();
      form.append('file', file, file.name);
      xhr.send(form);
    });

  window.CzReleaseAssets = { upload, cleanName };

  document.addEventListener('alpine:init', () => {
    Alpine.data('releaseAssetUpload', (opts) => ({
      uploads: [],
      error: '',
      names: opts.names,

      get busy() {
        return this.uploads.some((u) => !u.failed);
      },

      // Checked here as well as on the server so a refusal shows before the file is sent.
      refusal(file) {
        if (file.size > opts.maxBytes) return file.name + ' is larger than ' + opts.maxLabel + '.';
        if (this.names.length >= opts.maxAssets) return 'A release can have at most ' + opts.maxAssets + ' assets.';
        if (this.names.includes(cleanName(file.name))) return 'This release already has an asset named ' + cleanName(file.name) + '.';
        return '';
      },

      pick(event) {
        const file = event.target.files[0];
        event.target.value = '';
        if (!file) return;
        this.error = this.refusal(file);
        if (this.error) return;
        this.uploads = [{ name: file.name, pct: 0, failed: false }];
        // Alpine wraps the array: progress must update the proxied row, or nothing re-renders.
        const row = this.uploads[0];
        upload(opts.url, file, (f) => {
          row.pct = Math.round(f * 100);
        })
          .then(() => {
            try {
              sessionStorage.setItem('cz-toast', JSON.stringify({ type: 'success', message: 'Uploaded ' + cleanName(file.name) + '.' }));
            } catch (err) {}
            location.reload();
          })
          .catch((err) => {
            row.failed = true;
            this.uploads = [];
            this.error = err.message;
          });
      },
    }));
  });
})();
