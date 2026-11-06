/* CommentEditor -- shared comment composer for the mockups.
   Mirrors the live Templ components: CommentEditor > CommentEditorHeader +
   MarkdownEditor + CommentEditorActions.

   Requires markdown-editor.js to be loaded first (it supplies the inner editor).

   Each page uses:
     <div id="cz-comment-editor" data-avatar="MK"
          data-placeholder="Leave a comment — Markdown supported" data-rows="5">
       <button type="button" ...>Close pull request</button>
       <button type="submit" ...>Comment</button>
     </div>
     <script src="markdown-editor.js"></script>
     <script src="comment-editor.js"></script>

   The buttons inside the mount become the CommentEditorActions footer -- the
   page-specific slot. Markup below is a fixed build-time template. */
(function () {
  var mount = document.getElementById('cz-comment-editor');
  if (!mount) return;
  if (!window.CZMarkdownEditor) {
    console.error('comment-editor.js requires markdown-editor.js to be loaded first');
    return;
  }

  var avatar = mount.getAttribute('data-avatar') || '';
  var actions = mount.innerHTML; // page-specific action buttons
  var editor = window.CZMarkdownEditor.html({
    rows: mount.getAttribute('data-rows'),
    placeholder: mount.getAttribute('data-placeholder') || 'Leave a comment — Markdown supported'
  });

  mount.outerHTML = `
  <!-- CommentEditor -->
  <section class="mt-6" aria-label="Add a comment">
    <form class="rounded-md border border-border bg-card overflow-hidden">
      <!-- CommentEditorHeader -->
      <header class="px-3 py-2 border-b border-border bg-background flex items-center gap-2">
        <span class="h-6 w-6 rounded-full bg-muted border border-border grid place-items-center text-[10px] font-medium" aria-hidden="true">${avatar}</span>
        <span class="text-[13px] font-medium">Add a comment</span>
      </header>
      <!-- MarkdownEditor -->${editor}
      <!-- CommentEditorActions -->
      <div class="px-3 py-2 border-t border-border bg-muted/40 flex items-center gap-2">
        <p class="text-[11px] text-muted-foreground font-mono flex-1">Markdown supported · drop files to attach</p>
        ${actions}
      </div>
    </form>
  </section>`;
})();
