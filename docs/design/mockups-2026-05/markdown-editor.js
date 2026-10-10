/* MarkdownEditor -- shared write/preview editor for the mockups.
   Mirrors the live Templ component (internal/view/components/markdown_editor.templ).

   Standalone use -- the mount's <textarea> supplies the initial value:
     <div data-markdown-editor data-rows="18" data-placeholder="Edit the description…">
       <textarea>existing markdown…</textarea>
     </div>
     <script src="markdown-editor.js"></script>

   Add data-standalone="false" when the mount already sits inside a bordered
   card (the editor then drops its own border, e.g. the gist file card).

   Composed use -- other components call CZMarkdownEditor.html({ ... }) to embed
   the editor markup in their own template (see comment-editor.js).

   Auto-hydrates every [data-markdown-editor] mount on load. Markup below is a
   fixed build-time template; the only inputs are data-* attributes / opts. */
(function () {
  var TOOLBAR = [
    ['bold', 'Bold', '<path d="M6 4h8a4 4 0 014 4 4 4 0 01-4 4H6zM6 12h9a4 4 0 014 4 4 4 0 01-4 4H6z"/>'],
    ['italic', 'Italic', '<line x1="19" y1="4" x2="10" y2="4"/><line x1="14" y1="20" x2="5" y2="20"/><line x1="15" y1="4" x2="9" y2="20"/>'],
    ['code', 'Code', '<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>'],
    ['link', 'Insert link', '<path d="M10 13a5 5 0 007.07 0l3-3a5 5 0 00-7.07-7.07l-1 1"/><path d="M14 11a5 5 0 00-7.07 0l-3 3a5 5 0 007.07 7.07l1-1"/>']
  ].map(function (b) {
    return '<button type="button" data-md-action="' + b[0] + '" aria-label="' + b[1] + '" class="h-7 w-7 grid place-items-center rounded hover:elevated hover:text-current">'
      + '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false">' + b[2] + '</svg></button>';
  }).join('');

  function esc(s) {
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;');
  }

  /* Returns the MarkdownEditor markup as a string.
     opts: { rows, placeholder, value, standalone } */
  function html(opts) {
    opts = opts || {};
    var rows = parseInt(opts.rows, 10) || 5;
    var paneHeight = (rows * 22 + 24) + 'px';
    var placeholder = opts.placeholder || 'Leave a comment — Markdown supported';
    var shell = opts.standalone ? 'md-editor b border rounded-md surface overflow-hidden' : 'md-editor';
    return `
      <div class="${shell}" data-md-editor>
        <div class="flex items-center gap-2 b border-b px-3">
          <div role="tablist" aria-label="Editor mode" class="flex">
            <button type="button" role="tab" data-md-tab="write" aria-selected="true" class="px-3 py-2.5 border-b -mb-px tab-active font-medium text-[13px]">Write</button>
            <button type="button" role="tab" data-md-tab="preview" aria-selected="false" class="px-3 py-2.5 border-b -mb-px border-transparent fg2 hover:text-current text-[13px]">Preview</button>
          </div>
          <div class="ml-auto flex items-center gap-1 fg3" aria-label="Formatting">${TOOLBAR}</div>
        </div>
        <div data-md-pane="write">
          <textarea name="body" rows="${rows}" placeholder="${placeholder}" aria-label="Markdown editor" class="w-full px-3 py-3 text-[13px] mono leading-relaxed bg-transparent resize-none focus:outline-none placeholder:fg3" style="height:${paneHeight}">${esc(opts.value || '')}</textarea>
        </div>
        <div data-md-pane="preview" class="hidden px-4 py-3 prose" style="height:${paneHeight};overflow-y:auto">
          <p class="fg3 text-[13px]">Nothing to preview.</p>
        </div>
      </div>`;
  }

  /* Replaces a [data-markdown-editor] mount with the rendered editor. */
  function hydrate(mount) {
    var ta = mount.querySelector('textarea');
    mount.outerHTML = html({
      rows: mount.getAttribute('data-rows'),
      placeholder: mount.getAttribute('data-placeholder') || '',
      value: ta ? ta.value : '',
      standalone: mount.getAttribute('data-standalone') !== 'false'
    });
  }

  /* write/preview tabs + formatting toolbar -- delegated, so it covers every
     [data-md-editor] on the page, including ones rendered by comment-editor.js. */
  function setMode(ed, mode) {
    ed.querySelectorAll('[data-md-tab]').forEach(function (t) {
      var on = t.dataset.mdTab === mode;
      t.classList.toggle('tab-active', on);
      t.classList.toggle('font-medium', on);
      t.classList.toggle('border-transparent', !on);
      t.classList.toggle('fg2', !on);
      t.setAttribute('aria-selected', on ? 'true' : 'false');
    });
    ed.querySelector('[data-md-pane="write"]').classList.toggle('hidden', mode !== 'write');
    ed.querySelector('[data-md-pane="preview"]').classList.toggle('hidden', mode !== 'preview');
  }
  function wrap(ta, action) {
    var s = ta.selectionStart, e = ta.selectionEnd, sel = ta.value.slice(s, e);
    var pairs = { bold: ['**', '**'], italic: ['_', '_'], code: ['`', '`'] };
    var out = action === 'link' ? '[' + (sel || 'text') + '](url)'
      : (pairs[action] || ['', ''])[0] + (sel || 'text') + (pairs[action] || ['', ''])[1];
    ta.setRangeText(out, s, e, 'end');
    ta.focus();
  }
  document.addEventListener('click', function (e) {
    var tab = e.target.closest('[data-md-tab]');
    if (tab) { setMode(tab.closest('[data-md-editor]'), tab.dataset.mdTab); return; }
    var act = e.target.closest('[data-md-action]');
    if (act) {
      var ta = act.closest('[data-md-editor]').querySelector('textarea');
      if (ta) wrap(ta, act.dataset.mdAction);
    }
  });

  document.querySelectorAll('[data-markdown-editor]').forEach(hydrate);
  window.CZMarkdownEditor = { html: html };
})();
