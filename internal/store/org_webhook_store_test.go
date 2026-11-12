package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func seedWebhookOrg(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, "org_"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	return orgID
}

func seedOrgRepo(t *testing.T, db *sql.DB, orgID int64, orgName, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(
		`INSERT INTO repositories (owner_name, org_id, name) VALUES ($1, $2, $3) RETURNING id`, orgName, orgID, name,
	).Scan(&id); err != nil {
		t.Fatalf("seed org repo: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE id = $1`, id) })
	return id
}

func TestWebhookStore_OwnerCheck(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewWebhookStore(db)
	repoID, suffix := seedWebhookRepo(t, db)
	orgID := seedWebhookOrg(t, db, suffix)

	for name, wh := range map[string]*model.Webhook{
		"both set":    {RepoID: repoID, OrgID: orgID, URL: "https://example.test/h", Events: "push", Active: true},
		"neither set": {URL: "https://example.test/h", Events: "push", Active: true},
	} {
		if err := s.Create(ctx, wh); err == nil {
			t.Errorf("Create with %s must violate webhooks_one_owner", name)
		}
	}

	wh := &model.Webhook{OrgID: orgID, URL: "https://example.test/h", Events: "push", Active: true}
	if err := s.Create(ctx, wh); err != nil {
		t.Fatalf("Create org webhook: %v", err)
	}
	got, err := s.GetByID(ctx, wh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OrgID != orgID || got.RepoID != 0 {
		t.Errorf("GetByID = repo %d org %d; want repo 0 org %d", got.RepoID, got.OrgID, orgID)
	}
}

func TestWebhookStore_ListForRepoEvents_FollowsTheRepoOrg(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewWebhookStore(db)
	ownerRepoID, suffix := seedWebhookRepo(t, db)
	orgID := seedWebhookOrg(t, db, suffix)
	otherOrgID := seedWebhookOrg(t, db, suffix+"x")
	orgRepoID := seedOrgRepo(t, db, orgID, "org_"+suffix, "r1")

	own := createWebhook(t, s, orgRepoID, "https://example.test/own")
	orgHook := &model.Webhook{OrgID: orgID, URL: "https://example.test/org", Events: "push", Active: true}
	otherHook := &model.Webhook{OrgID: otherOrgID, URL: "https://example.test/other", Events: "push", Active: true}
	for _, wh := range []*model.Webhook{orgHook, otherHook} {
		if err := s.Create(ctx, wh); err != nil {
			t.Fatal(err)
		}
	}

	ids := func(repoID int64) map[int64]bool {
		hooks, err := s.ListForRepoEvents(ctx, repoID)
		if err != nil {
			t.Fatal(err)
		}
		m := map[int64]bool{}
		for _, h := range hooks {
			m[h.ID] = true
		}
		return m
	}
	if got := ids(orgRepoID); len(got) != 2 || !got[own.ID] || !got[orgHook.ID] {
		t.Errorf("org repo hooks = %v; want its own and the org's", got)
	}
	if got := ids(ownerRepoID); len(got) != 0 {
		t.Errorf("personal repo hooks = %v; want none", got)
	}

	if n, err := s.CountByOrg(ctx, orgID); err != nil || n != 1 {
		t.Errorf("CountByOrg = %d, %v; want 1", n, err)
	}
	// The repo's own list never shows the organization's hook.
	if hooks, _ := s.ListByRepo(ctx, orgRepoID); len(hooks) != 1 || hooks[0].ID != own.ID {
		t.Errorf("ListByRepo = %+v; want only the repo's own hook", hooks)
	}
	// A repo-scoped delete can't remove an organization hook.
	if err := s.Delete(ctx, orgHook.ID, orgRepoID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByID(ctx, orgHook.ID); err != nil {
		t.Errorf("org hook removed by a repo-scoped Delete: %v", err)
	}
	if err := s.DeleteForOrg(ctx, orgHook.ID, otherOrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByID(ctx, orgHook.ID); err != nil {
		t.Errorf("org hook removed by another org's DeleteForOrg: %v", err)
	}
}
