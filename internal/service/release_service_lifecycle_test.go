package service_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestReleaseService_Create_DraftHasNoPublishedAt(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t)
	ctx := context.Background()

	draft, err := svc.Create(ctx, owner, repo, "v1.0.0", "", "One", "notes", false, true, authorID)
	if err != nil {
		t.Fatal(err)
	}
	if !draft.IsDraft || draft.PublishedAt != nil {
		t.Errorf("draft = %+v, want IsDraft and nil PublishedAt", draft)
	}
	pub, err := svc.Create(ctx, owner, repo, "v1.1.0", "", "Two", "", false, false, authorID)
	if err != nil {
		t.Fatal(err)
	}
	if pub.PublishedAt == nil {
		t.Error("published release must have PublishedAt set")
	}
}

func TestReleaseService_Create_ExistingTagKeepsItsCommit(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t, "v0.9.0")
	r, err := svc.Create(context.Background(), owner, repo, "v0.9.0", "does-not-exist", "Pre-tagged", "", false, false, authorID)
	if err != nil {
		t.Fatalf("a tag that already exists must not need a valid target: %v", err)
	}
	if r.TagName != "v0.9.0" {
		t.Errorf("tag = %q", r.TagName)
	}
}

func TestReleaseService_Create_UnknownRepo(t *testing.T) {
	svc, owner, _, authorID := newReleaseSvc(t)
	_, err := svc.Create(context.Background(), owner, "nope", "v1", "", "", "", false, false, authorID)
	if err == nil || !strings.Contains(err.Error(), "repo not found") {
		t.Fatalf("err = %v, want repo not found", err)
	}
}

func TestReleaseService_UnknownRepo(t *testing.T) {
	svc, owner, _, _ := newReleaseSvc(t)
	ctx := context.Background()

	calls := map[string]func() error{
		"ListByRepo":    func() error { _, err := svc.ListByRepo(ctx, owner, "nope"); return err },
		"RecentForRepo": func() error { _, err := svc.RecentForRepo(ctx, owner, "nope", 3); return err },
		"GetByTag":      func() error { _, err := svc.GetByTag(ctx, owner, "nope", "v1"); return err },
		"GetByID":       func() error { _, err := svc.GetByID(ctx, owner, "nope", 1); return err },
		"GetLatest":     func() error { _, err := svc.GetLatest(ctx, owner, "nope"); return err },
		"Update":        func() error { _, err := svc.Update(ctx, owner, "nope", 1, "v1", "", "", false, false); return err },
		"EditName":      func() error { _, err := svc.EditName(ctx, owner, "nope", 1, "n"); return err },
		"Publish":       func() error { _, err := svc.Publish(ctx, owner, "nope", 1); return err },
		"EditPrerelease": func() error {
			_, err := svc.EditPrerelease(ctx, owner, "nope", 1, true)
			return err
		},
		"EditBody": func() error { _, err := svc.EditBody(ctx, owner, "nope", 1, "b"); return err },
		"Delete":   func() error { return svc.Delete(ctx, owner, "nope", 1) },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil || !strings.Contains(err.Error(), "repo not found") {
				t.Fatalf("err = %v, want repo not found", err)
			}
		})
	}
}

func TestReleaseService_UnknownReleaseID(t *testing.T) {
	svc, owner, repo, _ := newReleaseSvc(t)
	ctx := context.Background()

	calls := map[string]func() error{
		"GetByID":        func() error { _, err := svc.GetByID(ctx, owner, repo, -1); return err },
		"Update":         func() error { _, err := svc.Update(ctx, owner, repo, -1, "v1", "", "", false, false); return err },
		"EditName":       func() error { _, err := svc.EditName(ctx, owner, repo, -1, "n"); return err },
		"Publish":        func() error { _, err := svc.Publish(ctx, owner, repo, -1); return err },
		"EditPrerelease": func() error { _, err := svc.EditPrerelease(ctx, owner, repo, -1, true); return err },
		"EditBody":       func() error { _, err := svc.EditBody(ctx, owner, repo, -1, "b"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("err = %v, want wrapped sql.ErrNoRows", err)
			}
		})
	}
}

func TestReleaseService_GetByTag_And_GetByID_ScopedToRepo(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t)
	ctx := context.Background()
	r, err := svc.Create(ctx, owner, repo, "v1", "", "n", "", false, false, authorID)
	if err != nil {
		t.Fatal(err)
	}

	byID, err := svc.GetByID(ctx, owner, repo, r.ID)
	if err != nil || byID.TagName != "v1" {
		t.Fatalf("GetByID = %v, %v", byID, err)
	}
	if _, err := svc.GetByTag(ctx, owner, repo, "v-missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetByTag missing: err = %v, want sql.ErrNoRows", err)
	}

	otherSvc, otherOwner, otherRepo, _ := newReleaseSvc(t)
	if _, err := otherSvc.GetByID(ctx, otherOwner, otherRepo, r.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("a release id from another repo must not resolve, err = %v", err)
	}
}

func TestReleaseService_GetLatest_SkipsDrafts(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t)
	ctx := context.Background()

	if _, err := svc.GetLatest(ctx, owner, repo); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("no releases: err = %v, want sql.ErrNoRows", err)
	}
	if _, err := svc.Create(ctx, owner, repo, "v1", "", "", "", false, false, authorID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, owner, repo, "v2-draft", "", "", "", false, true, authorID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetLatest(ctx, owner, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.TagName != "v1" {
		t.Errorf("latest = %q, want v1 (drafts are skipped)", got.TagName)
	}
}

func TestReleaseService_CountPublished(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t)
	ctx := context.Background()
	rel, err := svc.Create(ctx, owner, repo, "v1", "", "", "", false, false, authorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, owner, repo, "v2", "", "", "", false, true, authorID); err != nil {
		t.Fatal(err)
	}
	n, err := svc.CountPublished(ctx, rel.RepoID)
	if err != nil || n != 1 {
		t.Errorf("CountPublished = %d, %v; want 1", n, err)
	}
}

func TestReleaseService_RecentForRepo_OrdersByPublishedThenCreated(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)

	old, err := svc.Create(ctx, owner, repo, "old", "", "", "", false, false, authorID)
	if err != nil {
		t.Fatal(err)
	}
	mid, err := svc.Create(ctx, owner, repo, "mid", "", "", "", false, false, authorID)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := svc.Create(ctx, owner, repo, "draft", "", "", "", false, true, authorID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	testutil.Exec(t, db, `UPDATE releases SET published_at = $2 WHERE id = $1`, old.ID, now.Add(-72*time.Hour))
	testutil.Exec(t, db, `UPDATE releases SET published_at = $2 WHERE id = $1`, mid.ID, now.Add(-48*time.Hour))
	testutil.Exec(t, db, `UPDATE releases SET created_at = $2 WHERE id = $1`, draft.ID, now.Add(-24*time.Hour))

	all, err := svc.RecentForRepo(ctx, owner, repo, 0)
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, r := range all {
		tags = append(tags, r.TagName)
	}
	if got := strings.Join(tags, ","); got != "draft,mid,old" {
		t.Errorf("order = %s, want draft,mid,old (created_at stands in when unpublished)", got)
	}

	limited, err := svc.RecentForRepo(ctx, owner, repo, 2)
	if err != nil || len(limited) != 2 || limited[0].TagName != "draft" {
		t.Errorf("limit 2 = %v, %v", limited, err)
	}
}

func TestReleaseService_Update(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t)
	ctx := context.Background()
	draft, err := svc.Create(ctx, owner, repo, "v1", "", "n", "b", false, true, authorID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.Create(ctx, owner, repo, "v2", "", "n", "b", false, false, authorID)
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Update(ctx, owner, repo, draft.ID, "v1-final", "Renamed", "new body", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.TagName != "v1-final" || got.Name != "Renamed" || got.Body != "new body" || !got.IsPrerelease || got.IsDraft || got.PublishedAt == nil {
		t.Errorf("updated = %+v", got)
	}
	firstPublished := *got.PublishedAt

	again, err := svc.Update(ctx, owner, repo, draft.ID, "v1-final", "Renamed", "new body", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !again.PublishedAt.Equal(firstPublished) {
		t.Error("re-saving a published release must keep its original PublishedAt")
	}

	if _, err := svc.Update(ctx, owner, repo, other.ID, "v1-final", "", "", false, false); err == nil {
		t.Error("renaming onto another release's tag must fail")
	}
}

func TestReleaseService_Publish(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t)
	ctx := context.Background()
	draft, err := svc.Create(ctx, owner, repo, "v1", "", "", "", false, true, authorID)
	if err != nil {
		t.Fatal(err)
	}

	pub, err := svc.Publish(ctx, owner, repo, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pub.IsDraft || pub.PublishedAt == nil {
		t.Errorf("published = %+v", pub)
	}
	if _, err := svc.Publish(ctx, owner, repo, draft.ID); !errors.Is(err, service.ErrReleaseAlreadyPublished) {
		t.Errorf("second publish: err = %v, want ErrReleaseAlreadyPublished", err)
	}
	stored, err := svc.GetByID(ctx, owner, repo, draft.ID)
	if err != nil || stored.IsDraft {
		t.Errorf("stored = %+v, %v", stored, err)
	}
}

func TestReleaseService_FieldEdits(t *testing.T) {
	svc, owner, repo, authorID := newReleaseSvc(t)
	ctx := context.Background()
	r, err := svc.Create(ctx, owner, repo, "v1", "", "old", "old body", false, false, authorID)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := svc.EditName(ctx, owner, repo, r.ID, "new name"); err != nil || got.Name != "new name" || got.Body != "old body" {
		t.Errorf("EditName = %+v, %v", got, err)
	}
	if got, err := svc.EditBody(ctx, owner, repo, r.ID, "new body"); err != nil || got.Body != "new body" || got.Name != "new name" {
		t.Errorf("EditBody = %+v, %v", got, err)
	}
	if got, err := svc.EditPrerelease(ctx, owner, repo, r.ID, true); err != nil || !got.IsPrerelease {
		t.Errorf("EditPrerelease = %+v, %v", got, err)
	}
	stored, err := svc.GetByID(ctx, owner, repo, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "new name" || stored.Body != "new body" || !stored.IsPrerelease {
		t.Errorf("stored = %+v", stored)
	}
}
