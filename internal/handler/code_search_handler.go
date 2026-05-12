package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageCodeSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	repoFilter := r.URL.Query().Get("repo")
	lang := r.URL.Query().Get("lang")
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	data := view.CodeSearchData{
		BasePage:   basePage(r, h.Services),
		Query:      q,
		RepoFilter: repoFilter,
		Lang:       lang,
		Page:       page,
	}

	if q != "" {
		var repoID *int64
		if repoFilter != "" {
			ownerName, repoName := splitOwnerRepo(repoFilter)
			if ownerName != "" && repoName != "" {
				if repo, err := h.Services.Repo.Get(r.Context(), ownerName, repoName); err == nil {
					repoID = &repo.ID
				}
			}
		}

		var langExt string
		if lang != "" {
			langExt = langToExt(lang)
		}

		results, total, err := h.Services.Index.Search(r.Context(), q, repoID, langExt, page, 20)
		if err != nil {
			slog.Warn("code search failed", "query", q, "error", err)
		}
		if results == nil {
			results = []model.CodeSearchResult{}
		}
		data.Results = results
		data.Total = total
	}

	h.render(w, r, pages.CodeSearch(data))
}

// splitOwnerRepo splits "owner/repo" into its two parts.
func splitOwnerRepo(s string) (string, string) {
	for i, c := range s {
		if c == '/' {
			return s[:i], s[i+1:]
		}
	}
	return "", ""
}

// langToExt maps a language name to its primary file extension for filtering.
func langToExt(lang string) string {
	switch lang {
	case "go":
		return ".go"
	case "python":
		return ".py"
	case "javascript":
		return ".js"
	case "typescript":
		return ".ts"
	case "rust":
		return ".rs"
	case "java":
		return ".java"
	case "ruby":
		return ".rb"
	case "c":
		return ".c"
	case "cpp", "c++":
		return ".cpp"
	default:
		if len(lang) > 0 && lang[0] == '.' {
			return lang
		}
		return "." + lang
	}
}
