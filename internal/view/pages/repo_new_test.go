package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// Picking an org as owner must switch the visibility radios to that org's
// default; the owner options carry the default for the form script to apply.
func TestRepoNew_OwnerOptionsCarryDefaultVisibility(t *testing.T) {
	data := view.RepoNewData{
		BasePage: view.BasePage{CurrentUser: &middleware.Claims{UserID: 1, Username: "alice"}},
		OwnedOrgs: []model.Organization{
			{ID: 1, Name: "privorg", DefaultRepoVisibility: "private"},
			{ID: 2, Name: "puborg", DefaultRepoVisibility: "public"},
		},
	}
	var sb strings.Builder
	if err := pages.RepoNew(data).Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := sb.String()

	for _, want := range []string{
		`&#34;alice&#34;:&#34;public&#34;`,
		`&#34;privorg&#34;:&#34;private&#34;`,
		`&#34;puborg&#34;:&#34;public&#34;`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("owner picker missing %s", want)
		}
	}
}
