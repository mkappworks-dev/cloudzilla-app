package service

import (
	"testing"

	"github.com/mkappworks/cloudzilla/internal/model"
)

func TestAutoMergeGuard(t *testing.T) {
	tests := []struct {
		name     string
		pr       model.PullRequest
		strategy string
		wantErr  string
	}{
		{
			name:     "valid open PR with ff",
			pr:       model.PullRequest{State: model.PRStateOpen, IsDraft: false},
			strategy: "ff",
			wantErr:  "",
		},
		{
			name:     "valid open PR with squash",
			pr:       model.PullRequest{State: model.PRStateOpen, IsDraft: false},
			strategy: "squash",
			wantErr:  "",
		},
		{
			name:     "draft PR blocked",
			pr:       model.PullRequest{State: model.PRStateOpen, IsDraft: true},
			strategy: "ff",
			wantErr:  "cannot enable auto-merge on a draft pull request",
		},
		{
			name:     "merged PR blocked",
			pr:       model.PullRequest{State: model.PRStateMerged},
			strategy: "ff",
			wantErr:  "auto-merge requires an open pull request",
		},
		{
			name:     "closed PR blocked",
			pr:       model.PullRequest{State: model.PRStateClosed},
			strategy: "ff",
			wantErr:  "auto-merge requires an open pull request",
		},
		{
			name:     "invalid strategy",
			pr:       model.PullRequest{State: model.PRStateOpen},
			strategy: "rebase",
			wantErr:  "strategy must be ff, merge, or squash",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := autoMergeGuard(&tt.pr, tt.strategy)
			if got != tt.wantErr {
				t.Errorf("autoMergeGuard() = %q, want %q", got, tt.wantErr)
			}
		})
	}
}
