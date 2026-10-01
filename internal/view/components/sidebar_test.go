package components_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

func TestAssigneeSidebar_UsernameStaysQuotedInHxVals(t *testing.T) {
	for _, name := range testutil.HostileNames {
		var sb strings.Builder
		if err := components.AssigneeSidebar("alice", "r", 1, "issues", nil, []string{name}, true).Render(context.Background(), &sb); err != nil {
			t.Fatalf("render: %v", err)
		}
		testutil.AssertNameStaysQuoted(t, sb.String(), name)
	}
}

func TestAssigneeSidebar_UnassignQueryEscapesUsername(t *testing.T) {
	var sb strings.Builder
	assignees := []model.User{{ID: 1, Username: "a&b"}}
	if err := components.AssigneeSidebar("alice", "r", 1, "issues", assignees, []string{"a&b"}, true).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(sb.String(), `hx-delete="/api/repos/alice/r/issues/1/assignees?username=a%26b"`) {
		t.Error("unassign URL must query-escape the username")
	}
}
