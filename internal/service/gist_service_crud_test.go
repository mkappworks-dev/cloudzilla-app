package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type gistEnv struct {
	svc     *service.GistService
	ownerID int64
	owner   string
	otherID int64
}

func newGistEnv(t *testing.T) gistEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, "gown"+suffix)
	otherID := testutil.SeedUser(t, db, "goth"+suffix)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM gists WHERE owner_id = ANY($1)`, []int64{ownerID, otherID}) })
	return gistEnv{
		svc:     service.NewGistService(store.NewGistStore(db)),
		ownerID: ownerID,
		owner:   "testuser_gown" + suffix,
		otherID: otherID,
	}
}

func gistFiles(names ...string) []model.GistFile {
	files := make([]model.GistFile, len(names))
	for i, n := range names {
		files[i] = model.GistFile{Filename: n, Content: "content of " + n}
	}
	return files
}

func TestGistService_CreateRejectsInvalidFilesBeforeStoring(t *testing.T) {
	e := newGistEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Create(ctx, e.ownerID, e.owner, "d", true, nil); err == nil {
		t.Error("Create with no files must fail")
	}
	n, err := e.svc.CountByUser(ctx, e.ownerID)
	if err != nil {
		t.Fatalf("CountByUser: %v", err)
	}
	if n != 0 {
		t.Errorf("rejected create left %d gists", n)
	}
}

func TestGistService_CreateGetRoundTrip(t *testing.T) {
	e := newGistEnv(t)
	ctx := context.Background()
	g, err := e.svc.Create(ctx, e.ownerID, e.owner, "desc", true, gistFiles("a.go", "b.md"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(g.ID) != 32 {
		t.Errorf("gist id = %q, want 32 hex chars", g.ID)
	}
	got, files, err := e.svc.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description != "desc" || !got.Public || got.OwnerID != e.ownerID {
		t.Errorf("gist = %+v", got)
	}
	if len(files) != 2 || files[0].Filename != "a.go" || files[1].Content != "content of b.md" {
		t.Errorf("files = %+v", files)
	}
	if _, _, err := e.svc.Get(ctx, "nonexistent"); err == nil {
		t.Error("Get of unknown id must fail")
	}
}

func TestGistService_UpdateRequiresOwner(t *testing.T) {
	e := newGistEnv(t)
	ctx := context.Background()
	g, err := e.svc.Create(ctx, e.ownerID, e.owner, "old", true, gistFiles("a.txt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := e.svc.Update(ctx, g.ID, e.otherID, "hijack", true, gistFiles("a.txt")); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("non-owner Update err = %v, want ErrForbidden", err)
	}
	if err := e.svc.Update(ctx, "missing", e.ownerID, "x", true, gistFiles("a.txt")); !errors.Is(err, service.ErrGistNotFound) {
		t.Errorf("unknown Update err = %v, want ErrGistNotFound", err)
	}
	if err := e.svc.Update(ctx, g.ID, e.ownerID, "x", true, nil); err == nil {
		t.Error("Update with no files must fail")
	}
	got, _, _ := e.svc.Get(ctx, g.ID)
	if got.Description != "old" {
		t.Errorf("failed updates changed description to %q", got.Description)
	}

	if err := e.svc.Update(ctx, g.ID, e.ownerID, "new", false, gistFiles("c.txt")); err != nil {
		t.Fatalf("owner Update: %v", err)
	}
	got, files, _ := e.svc.Get(ctx, g.ID)
	if got.Description != "new" || got.Public || len(files) != 1 || files[0].Filename != "c.txt" {
		t.Errorf("after update: %+v %+v", got, files)
	}
}

func TestGistService_DeleteRequiresOwner(t *testing.T) {
	e := newGistEnv(t)
	ctx := context.Background()
	g, err := e.svc.Create(ctx, e.ownerID, e.owner, "d", true, gistFiles("a.txt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := e.svc.Delete(ctx, g.ID, e.otherID); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("non-owner Delete err = %v, want ErrForbidden", err)
	}
	if err := e.svc.Delete(ctx, "missing", e.ownerID); !errors.Is(err, service.ErrGistNotFound) {
		t.Errorf("unknown Delete err = %v, want ErrGistNotFound", err)
	}
	if err := e.svc.Delete(ctx, g.ID, e.ownerID); err != nil {
		t.Fatalf("owner Delete: %v", err)
	}
	if _, _, err := e.svc.Get(ctx, g.ID); err == nil {
		t.Error("gist still readable after delete")
	}
}

func TestGistService_ListsCountsAndPaging(t *testing.T) {
	e := newGistEnv(t)
	ctx := context.Background()
	pub1, _ := e.svc.Create(ctx, e.ownerID, e.owner, "p1", true, gistFiles("a.txt", "b.txt"))
	pub2, _ := e.svc.Create(ctx, e.ownerID, e.owner, "p2", true, gistFiles("c.txt"))
	priv, err := e.svc.Create(ctx, e.ownerID, e.owner, "s", false, gistFiles("d.txt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if n, _ := e.svc.CountByUser(ctx, e.ownerID); n != 3 {
		t.Errorf("CountByUser = %d, want 3", n)
	}
	if n, _ := e.svc.CountPublicByOwner(ctx, e.ownerID); n != 2 {
		t.Errorf("CountPublicByOwner = %d, want 2", n)
	}
	if n, _ := e.svc.CountPrivateByUser(ctx, e.ownerID); n != 1 {
		t.Errorf("CountPrivateByUser = %d, want 1", n)
	}
	if n, err := e.svc.CountPublic(ctx); err != nil || n < 2 {
		t.Errorf("CountPublic = %d, %v; want at least 2", n, err)
	}

	// Out-of-range paging falls back to page 1 / 20 rows instead of a negative OFFSET.
	all, err := e.svc.ListByOwner(ctx, e.ownerID, 0, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("ListByOwner = %d, %v; want 3", len(all), err)
	}
	if capped, _ := e.svc.ListByOwner(ctx, e.ownerID, 1, 101); len(capped) != 3 {
		t.Errorf("oversized page size returned %d rows", len(capped))
	}
	one, _ := e.svc.ListByOwner(ctx, e.ownerID, 1, 1)
	two, _ := e.svc.ListByOwner(ctx, e.ownerID, 2, 1)
	if len(one) != 1 || len(two) != 1 || one[0].ID == two[0].ID {
		t.Errorf("pages 1 and 2 of size 1 overlap: %+v %+v", one, two)
	}

	pubs, err := e.svc.ListPublicByOwner(ctx, e.ownerID, -1, 0)
	if err != nil || len(pubs) != 2 {
		t.Fatalf("ListPublicByOwner = %d, %v; want 2", len(pubs), err)
	}
	for _, g := range pubs {
		if g.ID == priv.ID {
			t.Error("ListPublicByOwner leaked a private gist")
		}
	}

	privs, err := e.svc.ListPrivateByOwner(ctx, e.ownerID, "", 0, 0)
	if err != nil || len(privs) != 1 || privs[0].ID != priv.ID {
		t.Fatalf("ListPrivateByOwner = %+v, %v", privs, err)
	}

	rows, err := e.svc.ListWithCounts(ctx, e.owner, "", 0, 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("ListWithCounts = %d, %v; want the 2 public gists", len(rows), err)
	}
	fileCounts := map[string]int64{}
	for _, r := range rows {
		fileCounts[r.ID] = r.FileCount
	}
	if fileCounts[pub1.ID] != 2 || fileCounts[pub2.ID] != 1 {
		t.Errorf("file counts = %v", fileCounts)
	}

	names, err := e.svc.LoadFilenames(ctx, []string{pub1.ID, priv.ID})
	if err != nil {
		t.Fatalf("LoadFilenames: %v", err)
	}
	if strings.Join(names[pub1.ID], ",") != "a.txt,b.txt" || len(names[priv.ID]) != 1 {
		t.Errorf("filenames = %v", names)
	}
}
