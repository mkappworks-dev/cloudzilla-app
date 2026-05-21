package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func renderOrganizations(t *testing.T, data view.OrgListData) string {
	t.Helper()
	var sb strings.Builder
	if err := pages.Organizations(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// The listing must show each org with its role badge, member count, and the
// New-organization CTA.
func TestOrganizations_RendersEntries(t *testing.T) {
	data := view.OrgListData{
		Entries: []view.OrgListEntry{
			{
				Org:         model.Organization{ID: 1, Name: "acme"},
				Role:        model.OrgRoleOwner,
				MemberCount: 3,
			},
			{
				Org:         model.Organization{ID: 2, Name: "globex"},
				Role:        model.OrgRoleMember,
				MemberCount: 1,
			},
		},
	}
	out := renderOrganizations(t, data)

	for _, want := range []string{
		"acme",
		"globex",
		"Owner",
		"Member",
		"New organization",
		"3 members",
		"1 member",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("organizations listing missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// With no memberships the empty state must render.
func TestOrganizations_EmptyState(t *testing.T) {
	out := renderOrganizations(t, view.OrgListData{})

	if !strings.Contains(out, "not a member of any organizations") {
		t.Errorf("organizations listing missing empty state\n--- output ---\n%s", out)
	}
}
