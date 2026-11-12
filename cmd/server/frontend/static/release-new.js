// release-new.js — the `releaseNewForm` Alpine component on the new-release page: creates the release,
// then uploads the queued files one by one through window.CzReleaseAssets (release-assets.js).
(() => {
  const csrf = () => (document.cookie.match(/csrf_token=([^;]+)/) || [])[1] || '';

  const humanSize = (n) => {
    if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1).replace(/\.0$/, '') + ' GiB';
    if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1).replace(/\.0$/, '') + ' MiB';
    if (n >= 1 << 10) return Math.round(n / (1 << 10)) + ' KiB';
    return n + ' bytes';
  };

  document.addEventListener('alpine:init', () => {
    Alpine.data('releaseNewForm', (opts) => ({
      queue: [],
      notices: [],
      dragging: false,
      busy: false,
      formError: '',
      status: '',
      created: null,

      get hasFailures() {
        return this.queue.some((q) => q.state === 'failed');
      },

      size: humanSize,

      get detailUrl() {
        return this.created ? opts.detailBase + encodeURIComponent(this.created.tag_name) : '#';
      },

      refusal(file) {
        const name = window.CzReleaseAssets.cleanName(file.name);
        if (file.size > opts.maxBytes) return file.name + ' is larger than ' + opts.maxLabel + '.';
        if (this.queue.length >= opts.maxAssets) return 'A release can have at most ' + opts.maxAssets + ' assets.';
        if (this.queue.some((q) => q.cleanName === name)) return 'A file named ' + name + ' is already in the list.';
        return '';
      },

      add(files) {
        const notices = [];
        for (const file of files) {
          const why = this.refusal(file);
          if (why) notices.push(why);
          else this.queue.push({ file, name: file.name, cleanName: window.CzReleaseAssets.cleanName(file.name), size: file.size, state: 'queued', pct: 0, error: '' });
        }
        this.notices = notices;
      },

      pick(event) {
        this.add(Array.from(event.target.files));
        event.target.value = '';
      },

      drop(event) {
        this.dragging = false;
        if (!this.created) this.add(Array.from(event.dataTransfer.files));
      },

      remove(index) {
        const [gone] = this.queue.splice(index, 1);
        this.notices = [];
        this.status = 'Removed ' + gone.name + '.';
      },

      fail(message, field) {
        const target = field && this.$root.elements[field];
        if (target && target.id) {
          CzFields.show(target, message);
          target.focus();
        } else {
          this.formError = message;
        }
      },

      async submit(event) {
        if (this.busy || this.created) return;
        const form = this.$root;
        const draft = event.submitter && event.submitter.dataset.draft === 'true';
        const data = new FormData(form);
        this.formError = '';
        this.busy = true;
        let res;
        try {
          res = await fetch(opts.createUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf() },
            body: JSON.stringify({
              tag_name: data.get('tag_name'),
              name: data.get('name'),
              target: data.get('target'),
              body: data.get('body'),
              is_prerelease: data.get('is_prerelease') === 'true',
              is_draft: draft,
            }),
          });
        } catch (err) {
          this.busy = false;
          this.fail('Could not reach the server. Check your connection and try again.');
          return;
        }
        const text = await res.text();
        let body = {};
        try {
          body = JSON.parse(text);
        } catch (err) {}
        if (!res.ok) {
          this.busy = false;
          this.fail(body.error || 'Could not create the release (HTTP ' + res.status + ').', body.field);
          return;
        }
        this.created = body;
        if (!this.queue.length) {
          location.href = opts.listUrl;
          return;
        }
        await this.uploadAll(body);
      },

      async uploadAll(release) {
        const url = opts.createUrl + '/' + release.id + '/assets';
        for (let i = 0; i < this.queue.length; i++) {
          const item = this.queue[i];
          if (item.state === 'done') continue;
          item.state = 'uploading';
          item.pct = 0;
          item.error = '';
          this.status = 'Uploading ' + item.name + ' (' + (i + 1) + ' of ' + this.queue.length + ').';
          try {
            await window.CzReleaseAssets.upload(url, item.file, (f) => {
              item.pct = Math.round(f * 100);
            });
            item.state = 'done';
            item.pct = 100;
          } catch (err) {
            item.state = 'failed';
            item.error = err.message;
          }
        }
        this.busy = false;
        if (this.hasFailures) {
          const failed = this.queue.filter((q) => q.state === 'failed').length;
          this.status = 'The release was created, but ' + failed + (failed === 1 ? ' file' : ' files') + " didn't upload.";
          return;
        }
        location.href = opts.detailBase + encodeURIComponent(release.tag_name);
      },
    }));
  });
})();
