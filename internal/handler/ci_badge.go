package handler

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// ciBadges builds the status badge of every SHA that has statuses, with one query however many SHAs there are. A
// failure drops the badges rather than the page.
func (h *Handler) ciBadges(ctx context.Context, repo *model.Repository, shas []string) map[string]components.CIBadge {
	cis, err := h.Services.CommitStatus.CIBySHAs(ctx, repo.ID, shas)
	if err != nil {
		slog.Warn("ci badges: status lookup failed", "repo_id", repo.ID, "error", err)
		return nil
	}
	out := make(map[string]components.CIBadge, len(cis))
	for sha, ci := range cis {
		state := string(ci.State)
		label := components.RunStatusLabel(state)
		if ci.RunNumber > 0 && h.Cfg.CI.Enabled {
			out[sha] = components.CIBadge{State: state, Href: actionsBase(repo) + "/runs/" + strconv.Itoa(ci.RunNumber), Title: label + " — latest run"}
			continue
		}
		out[sha] = components.CIBadge{State: state, Href: codeurl.Path(repo.OwnerName, repo.Name, "commit", sha, ""), Title: label + " — commit statuses"}
	}
	return out
}

func (h *Handler) ciBadge(ctx context.Context, repo *model.Repository, sha string) components.CIBadge {
	return h.ciBadges(ctx, repo, []string{sha})[sha]
}
