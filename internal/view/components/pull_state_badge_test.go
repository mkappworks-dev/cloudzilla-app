package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestPullStateBadge_Variants(t *testing.T) {
	cases := []struct {
		state    model.PRState
		isDraft  bool
		wantText string
	}{
		{model.PRStateOpen, false, "Open"},
		{model.PRStateMerged, false, "Merged"},
		{model.PRStateClosed, false, "Closed"},
		{model.PRStateOpen, true, "Draft"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		if err := PullStateBadge(tc.state, tc.isDraft).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(buf.String(), tc.wantText) {
			t.Errorf("state=%v draft=%v: missing %q in %s", tc.state, tc.isDraft, tc.wantText, buf.String())
		}
	}
}
