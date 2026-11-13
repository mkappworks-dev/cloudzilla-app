package pages_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

var hxDeleteTag = regexp.MustCompile(`<button[^>]*hx-delete="[^"]*"[^>]*>`)

// A delete or create swaps the list for the handler's fragment, so a row that renders differently there changes under
// the user's cursor and can lose its confirm.
func TestListFragments_MatchThePageMarkup(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	expires := created.AddDate(0, 1, 0)

	keys := []model.SSHKey{
		{ID: 7, Title: "laptop <work>", Fingerprint: "SHA256:abc", CreatedAt: created},
		{ID: 9, Title: "ci", Fingerprint: "SHA256:def", CreatedAt: created},
	}
	past := created.AddDate(0, -1, 0)
	signing := view.SigningKeysFragData{
		GPGKeys: []model.GPGKey{
			{ID: 1, KeyID: "3AA5C34371567BD2", Fingerprint: "4AEE18F83AFDEB23B1C97D043AA5C3437156 7BD2", Emails: []string{"Ada@Example.test", "ada@work.test"}, CreatedAt: created, Subkeys: []model.GPGSubkey{{KeyID: "4F2A9E01C77B3D65", CanSign: true}}},
			{ID: 2, KeyID: "9C21D0B7E4A68F13", Fingerprint: "71D30B5A92CE44F108A76E2D9C21D0B7E4A68F13", CreatedAt: created, ExpiresAt: &past},
		},
		SSHKeys:          []model.SSHSigningKey{{ID: 5, Title: "laptop <work>", Fingerprint: "SHA256:abc", CreatedAt: created}},
		VerifiedEmails:   map[string]bool{"ada@example.test": true},
		AuthFingerprints: map[string]bool{"SHA256:abc": true},
	}
	tokens := []model.AccessToken{
		{ID: 3, Name: "deploy", LastEight: "abcd1234", Scopes: []string{"repo:read", "repo:write"}, ExpiresAt: &expires, SigningKey: "ssh-ed25519 AAAA", Targets: []string{"ada/hollow"}},
		{ID: 4, Name: "scratch", LastEight: "efgh5678"},
	}
	invitations := []model.Invitation{
		{ID: 1, Email: "new@example.test", Token: "tok1", ExpiresAt: expires},
		{ID: 2, Email: "done@example.test", Token: "tok2", ExpiresAt: expires, AcceptedAt: &created},
	}
	refs := view.RefsData{
		Repo:     model.Repository{DefaultBranch: "main"},
		Owner:    "ada",
		RepoName: "hollow",
		Branches: []service.BranchInfo{{Name: "main", Hash: "1234567", IsDefault: true}, {Name: "feat/x y", Hash: "89abcde"}},
		Tags:     []service.TagInfo{{Name: "v1.0.0", Hash: "1234567"}},
		CanWrite: true,
	}

	cases := []struct {
		name, id    string
		page, frag  templ.Component
		wantDeletes int
	}{
		{
			name: "ssh keys", id: "ssh-keys-list",
			page:        pages.Settings(view.SettingsData{SSHKeys: keys}),
			frag:        fragments.SSHKeysList(view.SSHKeysFragData{SSHKeys: keys}),
			wantDeletes: 2,
		},
		{
			name: "signing keys", id: "signing-keys-list",
			page:        pages.Settings(view.SettingsData{SigningKeys: signing}),
			frag:        fragments.SigningKeysList(signing),
			wantDeletes: 3,
		},
		{
			name: "empty signing keys", id: "signing-keys-list",
			page: pages.Settings(view.SettingsData{}),
			frag: fragments.SigningKeysList(view.SigningKeysFragData{}),
		},
		{
			name: "tokens", id: "tokens-list",
			page:        pages.Settings(view.SettingsData{Tokens: tokens}),
			frag:        fragments.TokensList(view.TokensListFragData{Tokens: tokens}),
			wantDeletes: 2,
		},
		{
			name: "invitations", id: "admin-invitations-list",
			page:        pages.AdminSettings(view.AdminSettingsData{Invitations: invitations}),
			frag:        fragments.AdminInvitations(view.AdminInvitationsFragData{Invitations: invitations}),
			wantDeletes: 1,
		},
		{
			name: "branches", id: "branches-list",
			page:        pages.Refs(refs),
			frag:        fragments.BranchesList(view.BranchesFragData{Owner: "ada", RepoName: "hollow", Branches: refs.Branches, CanWrite: true, DefaultBranch: "main"}),
			wantDeletes: 1,
		},
		{
			name: "tags", id: "tags-list",
			page:        pages.Refs(refs),
			frag:        fragments.TagsList(view.TagsFragData{Owner: "ada", RepoName: "hollow", Tags: refs.Tags, CanWrite: true}),
			wantDeletes: 1,
		},
		{
			name: "empty ssh keys", id: "ssh-keys-list",
			page: pages.Settings(view.SettingsData{}),
			frag: fragments.SSHKeysList(view.SSHKeysFragData{}),
		},
		{
			name: "empty invitations", id: "admin-invitations-list",
			page: pages.AdminSettings(view.AdminSettingsData{}),
			frag: fragments.AdminInvitations(view.AdminInvitationsFragData{}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fromPage := divByID(t, renderComponent(t, tc.page), tc.id)
			frag := strings.TrimSpace(renderComponent(t, tc.frag))
			if fromPage != frag {
				t.Errorf("fragment differs from the page's #%s\npage:\n%s\nfragment:\n%s", tc.id, fromPage, frag)
			}

			deletes := hxDeleteTag.FindAllString(frag, -1)
			if len(deletes) != tc.wantDeletes {
				t.Fatalf("fragment has %d delete buttons, want %d", len(deletes), tc.wantDeletes)
			}
			for _, b := range deletes {
				if !strings.Contains(b, `hx-confirm="`) || !strings.Contains(b, `data-confirm-title="`) {
					t.Errorf("delete button without hx-confirm and data-confirm-title: %s", b)
				}
			}
		})
	}
}

func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// divByID returns the <div> carrying id, through its matching </div>.
func divByID(t *testing.T, html, id string) string {
	t.Helper()
	at := strings.Index(html, `id="`+id+`"`)
	if at < 0 {
		t.Fatalf("no element with id %q", id)
	}
	start := strings.LastIndex(html[:at], "<div")
	depth := 0
	for i := start; i < len(html); i++ {
		switch {
		case strings.HasPrefix(html[i:], "<div"):
			depth++
		case strings.HasPrefix(html[i:], "</div>"):
			depth--
			if depth == 0 {
				return html[start : i+len("</div>")]
			}
		}
	}
	t.Fatalf("#%s is never closed", id)
	return ""
}
