package pages

import (
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

func linkedPullsView(pulls []model.PullRequest) []view.LinkedPull {
	out := make([]view.LinkedPull, 0, len(pulls))
	for _, p := range pulls {
		out = append(out, view.LinkedPull{Number: p.Number, Title: p.Title, State: string(p.State)})
	}
	return out
}
