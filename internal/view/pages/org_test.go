package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func renderOrg(t *testing.T, data view.OrgData) string {
	t.Helper()
	var sb strings.Builder
	if err := pages.Org(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// The org detail page must render the header, the Repositories and People
// sections, and the contained repo and member.
func TestOrg_RendersHeaderAndSections(t *testing.T) {
	data := view.OrgData{
		Org: model.Organization{ID: 1, Name: "acme", DisplayName: "Acme Inc"},
		Repos: []model.Repository{
			{ID: 1, OwnerName: "acme", Name: "rocket"},
		},
		Members: []model.OrgMember{
			{ID: 1, OrgID: 1, UserID: 7, Username: "alice", Role: model.OrgRoleOwner},
		},
	}
	out := renderOrg(t, data)

	for _, want := range []string{
		"acme",
		"rocket",
		"alice",
		"Repositories",
		"People",
		"1 repository",
		"1 member",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("org detail page missing %q\n--- output ---\n%s", want, out)
		}
	}
}
