package handler

import (
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestPullDiffRefs(t *testing.T) {
	pr := model.PullRequest{BaseBranch: "main", HeadBranch: "feature", MergedBaseSHA: "b1", MergedHeadSHA: "h1"}
	for _, state := range []model.PRState{model.PRStateOpen, model.PRStateClosed} {
		pr.State = state
		if base, head, known := pullDiffRefs(&pr); base != "main" || head != "feature" || !known {
			t.Errorf("%s PR = %q, %q, %v; want its branches", state, base, head, known)
		}
	}

	pr.State = model.PRStateMerged
	if base, head, known := pullDiffRefs(&pr); base != "b1" || head != "h1" || !known {
		t.Errorf("merged PR = %q, %q, %v; want the recorded tips", base, head, known)
	}

	pr.MergedBaseSHA, pr.MergedHeadSHA = "", ""
	if _, _, known := pullDiffRefs(&pr); known {
		t.Error("a merged PR with no recorded range must not fall back to its merged branches")
	}
}
