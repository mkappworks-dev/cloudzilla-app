package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// OrgService.CreateRepo is owner-only and audit_log rows outlive the org, so the copy must not promise otherwise.
func TestOrgSettings_CopyMatchesPermissions(t *testing.T) {
	var sb strings.Builder
	data := view.OrgSettingsData{
		Org:         model.Organization{ID: 1, Name: "acme"},
		Members:     []model.OrgMember{{ID: 1, OrgID: 1, UserID: 7, Username: "alice", Role: model.OrgRoleOwner}},
		MemberCount: 1,
	}
	if err := pages.OrgSettings(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	for _, stale := range []string{
		"Members can create",
		"members can contribute",
		"when a member creates",
		"audit history",
	} {
		if strings.Contains(out, stale) {
			t.Errorf("org settings still says %q", stale)
		}
	}
	for _, want := range []string{
		"when an owner creates a repo here",
		"Audit log entries about the org are kept",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("org settings missing %q", want)
		}
	}
}
