package handler

import (
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
)

// MarkdownPreview renders submitted markdown to HTML for the editor Preview tab.
func (h *Handler) MarkdownPreview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if strings.TrimSpace(r.FormValue("body")) == "" {
		_, _ = w.Write([]byte(`<p class="text-sm text-muted-foreground italic">Nothing to preview.</p>`))
		return
	}
	_, _ = w.Write([]byte(markdown.Render(r.FormValue("body"))))
}
