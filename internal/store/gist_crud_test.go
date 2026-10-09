package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type gistFixture struct {
	db      *sql.DB
	gs      *store.GistStore
	ownerID int64
	owner   string
	prefix  string
}

func newGistFixture(t *testing.T) gistFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	return gistFixture{
		db:      db,
		gs:      store.NewGistStore(db),
		ownerID: testutil.SeedUser(t, db, suffix),
		owner:   "testuser_" + suffix,
		prefix:  "g" + suffix + "_",
	}
}

// add creates a gist whose created_at is age in the past, so list ordering is deterministic.
func (f gistFixture) add(t *testing.T, id, desc string, public bool, age time.Duration, filenames ...string) *model.Gist {
	t.Helper()
	g := &model.Gist{ID: f.prefix + id, OwnerID: f.ownerID, OwnerName: f.owner, Description: desc, Public: public}
	var files []model.GistFile
	for _, name := range filenames {
		files = append(files, model.GistFile{Filename: name, Content: "content of " + name})
	}
	if err := f.gs.Create(context.Background(), g, files); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
	testutil.Exec(t, f.db, `UPDATE gists SET created_at = NOW() - make_interval(secs => $1), updated_at = NOW() - make_interval(secs => $1) WHERE id = $2`,
		age.Seconds(), g.ID)
	return g
}

func gistIDs(gs []model.Gist) []string {
	ids := make([]string, len(gs))
	for i, g := range gs {
		ids[i] = g.ID
	}
	return ids
}

func (f gistFixture) ids(rest ...string) []string {
	out := make([]string, len(rest))
	for i, r := range rest {
		out[i] = f.prefix + r
	}
	return out
}

func TestGistStore_Create_DuplicateIDRejected(t *testing.T) {
	f := newGistFixture(t)
	f.add(t, "a", "first", true, 0, "a.txt")

	err := f.gs.Create(context.Background(), &model.Gist{ID: f.prefix + "a", OwnerID: f.ownerID, OwnerName: f.owner}, nil)
	wantUniqueViolation(t, "duplicate gist id", err)
}

func TestGistStore_Create_DuplicateFilenameRollsBack(t *testing.T) {
	f := newGistFixture(t)
	ctx := context.Background()
	g := &model.Gist{ID: f.prefix + "dup", OwnerID: f.ownerID, OwnerName: f.owner, Public: true}
	files := []model.GistFile{{Filename: "a.txt"}, {Filename: "a.txt"}}

	wantUniqueViolation(t, "duplicate filename", f.gs.Create(ctx, g, files))
	if _, _, err := f.gs.Get(ctx, g.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("gist row survived a failed create: %v", err)
	}
}

func TestGistStore_Get(t *testing.T) {
	f := newGistFixture(t)
	ctx := context.Background()
	parent := f.add(t, "parent", "origin", true, 0, "z.go", "a.go", "m.go")

	fork := &model.Gist{ID: f.prefix + "fork", OwnerID: f.ownerID, OwnerName: f.owner, Public: true}
	if err := f.gs.Create(ctx, fork, nil); err != nil {
		t.Fatalf("Create fork: %v", err)
	}
	testutil.Exec(t, f.db, `UPDATE gists SET forked_from_id = $1 WHERE id = $2`, parent.ID, fork.ID)

	g, files, err := f.gs.Get(ctx, parent.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if g.Description != "origin" || g.OwnerID != f.ownerID || g.OwnerName != f.owner || !g.Public || g.ForkedFromID != nil {
		t.Errorf("gist = %+v", g)
	}
	var names []string
	for _, file := range files {
		names = append(names, file.Filename)
		if file.GistID != parent.ID || file.Content != "content of "+file.Filename {
			t.Errorf("file = %+v", file)
		}
	}
	if want := []string{"z.go", "a.go", "m.go"}; !slices.Equal(names, want) {
		t.Errorf("files = %v, want insertion order %v", names, want)
	}

	forked, noFiles, err := f.gs.Get(ctx, fork.ID)
	if err != nil {
		t.Fatalf("Get fork: %v", err)
	}
	if forked.ForkedFromID == nil || *forked.ForkedFromID != parent.ID || len(noFiles) != 0 {
		t.Errorf("fork = %+v, files %v", forked, noFiles)
	}

	if _, _, err := f.gs.Get(ctx, f.prefix+"missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown id: %v, want sql.ErrNoRows", err)
	}
}

func TestGistStore_ListByOwner_PagedNewestFirst(t *testing.T) {
	f := newGistFixture(t)
	ctx := context.Background()
	f.add(t, "old", "", true, 3*time.Hour)
	f.add(t, "mid", "", false, 2*time.Hour)
	f.add(t, "new", "", true, time.Hour)

	tests := []struct {
		name           string
		page, pageSize int
		want           []string
	}{
		{"first page", 1, 2, f.ids("new", "mid")},
		{"second page", 2, 2, f.ids("old")},
		{"past the end", 3, 2, nil},
	}
	for _, tc := range tests {
		got, err := f.gs.ListByOwner(ctx, f.ownerID, tc.page, tc.pageSize)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !slices.Equal(gistIDs(got), tc.want) && (len(got) != 0 || len(tc.want) != 0) {
			t.Errorf("%s: got %v, want %v", tc.name, gistIDs(got), tc.want)
		}
	}
}

func TestGistStore_ListPublicByOwner_ExcludesPrivate(t *testing.T) {
	f := newGistFixture(t)
	ctx := context.Background()
	f.add(t, "pub1", "", true, 2*time.Hour)
	f.add(t, "priv", "", false, time.Hour)
	f.add(t, "pub2", "", true, 3*time.Hour)

	got, err := f.gs.ListPublicByOwner(ctx, f.ownerID, 1, 10)
	if err != nil {
		t.Fatalf("ListPublicByOwner: %v", err)
	}
	if want := f.ids("pub1", "pub2"); !slices.Equal(gistIDs(got), want) {
		t.Errorf("got %v, want %v", gistIDs(got), want)
	}
	if n, err := f.gs.CountPublicByOwner(ctx, f.ownerID); err != nil || n != 2 {
		t.Errorf("CountPublicByOwner = %d, %v; want 2", n, err)
	}
	if n, err := f.gs.CountPublicByOwner(ctx, -1); err != nil || n != 0 {
		t.Errorf("CountPublicByOwner unknown owner = %d, %v; want 0", n, err)
	}
}

func TestGistStore_ListPrivateByOwner_Sorting(t *testing.T) {
	f := newGistFixture(t)
	ctx := context.Background()
	f.add(t, "b", "Banana", false, 3*time.Hour)
	f.add(t, "a", "apple", false, 2*time.Hour)
	f.add(t, "c", "Cherry", false, time.Hour)
	f.add(t, "pub", "Public", true, 0)
	testutil.Exec(t, f.db, `UPDATE gists SET updated_at = NOW() - interval '10 minutes' WHERE id = $1`, f.prefix+"b")

	tests := []struct {
		sortBy string
		want   []string
	}{
		{"created", f.ids("c", "a", "b")},
		{"name", f.ids("a", "b", "c")},
		{"", f.ids("b", "c", "a")},
		{"bogus; DROP TABLE gists", f.ids("b", "c", "a")},
	}
	for _, tc := range tests {
		got, err := f.gs.ListPrivateByOwner(ctx, f.ownerID, tc.sortBy, 1, 10)
		if err != nil {
			t.Fatalf("sort %q: %v", tc.sortBy, err)
		}
		if !slices.Equal(gistIDs(got), tc.want) {
			t.Errorf("sort %q: got %v, want %v", tc.sortBy, gistIDs(got), tc.want)
		}
	}
}

func TestGistStore_Update_ReplacesFiles(t *testing.T) {
	f := newGistFixture(t)
	ctx := context.Background()
	g := f.add(t, "u", "before", true, time.Hour, "old.txt", "keep.txt")
	beforeGist, _, err := f.gs.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	g.Description, g.Public = "after", false
	files := []model.GistFile{{Filename: "keep.txt", Content: "new content"}, {Filename: "added.txt", Content: "x"}}
	if err := f.gs.Update(ctx, g, files); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, gotFiles, err := f.gs.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description != "after" || got.Public {
		t.Errorf("gist = %+v", got)
	}
	if !got.UpdatedAt.After(beforeGist.UpdatedAt) {
		t.Errorf("UpdatedAt %v not after %v", got.UpdatedAt, beforeGist.UpdatedAt)
	}
	if !got.CreatedAt.Equal(beforeGist.CreatedAt) {
		t.Errorf("CreatedAt changed: %v -> %v", beforeGist.CreatedAt, got.CreatedAt)
	}
	var names []string
	for _, file := range gotFiles {
		names = append(names, file.Filename)
	}
	if want := []string{"keep.txt", "added.txt"}; !slices.Equal(names, want) || gotFiles[0].Content != "new content" {
		t.Errorf("files = %+v, want %v with replaced content", gotFiles, want)
	}
}

func TestGistStore_Update_DuplicateFilenameRollsBack(t *testing.T) {
	f := newGistFixture(t)
	ctx := context.Background()
	g := f.add(t, "rb", "original", true, 0, "keep.txt")

	g.Description = "changed"
	err := f.gs.Update(ctx, g, []model.GistFile{{Filename: "x"}, {Filename: "x"}})
	wantUniqueViolation(t, "duplicate filename on update", err)

	got, files, err := f.gs.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description != "original" || len(files) != 1 || files[0].Filename != "keep.txt" {
		t.Errorf("update not rolled back: %+v, %+v", got, files)
	}
}

func TestGistStore_Delete_CascadesFilesAndForkLink(t *testing.T) {
	f := newGistFixture(t)
	ctx := context.Background()
	parent := f.add(t, "p", "", true, 0, "a.txt")
	child := f.add(t, "c", "", true, 0, "b.txt")
	testutil.Exec(t, f.db, `UPDATE gists SET forked_from_id = $1 WHERE id = $2`, parent.ID, child.ID)

	if err := f.gs.Delete(ctx, parent.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := f.gs.Get(ctx, parent.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("gist still present: %v", err)
	}
	var files int
	if err := f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gist_files WHERE gist_id = $1`, parent.ID).Scan(&files); err != nil || files != 0 {
		t.Errorf("orphaned files = %d, %v", files, err)
	}
	got, _, err := f.gs.Get(ctx, child.ID)
	if err != nil || got.ForkedFromID != nil {
		t.Errorf("fork after parent delete = %+v, %v; want ForkedFromID nil", got, err)
	}
	if err := f.gs.Delete(ctx, parent.ID); err != nil {
		t.Errorf("deleting absent gist: %v", err)
	}
}

func TestGistStore_LoadFilenames(t *testing.T) {
	f := newGistFixture(t)
	ctx := context.Background()
	a := f.add(t, "a", "", true, 0, "two.txt", "one.txt")
	b := f.add(t, "b", "", true, 0, "only.txt")
	empty := f.add(t, "e", "", true, 0)

	t.Run("no ids", func(t *testing.T) {
		got, err := f.gs.LoadFilenames(ctx, nil)
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("got %v, %v; want empty non-nil map", got, err)
		}
	})

	t.Run("batches by gist in insertion order", func(t *testing.T) {
		got, err := f.gs.LoadFilenames(ctx, []string{a.ID, b.ID, empty.ID, f.prefix + "missing"})
		if err != nil {
			t.Fatalf("LoadFilenames: %v", err)
		}
		if !slices.Equal(got[a.ID], []string{"two.txt", "one.txt"}) || !slices.Equal(got[b.ID], []string{"only.txt"}) {
			t.Errorf("got %v", got)
		}
		if _, ok := got[empty.ID]; ok {
			t.Error("gist without files must be absent from the map")
		}
		if len(got) != 2 {
			t.Errorf("len = %d, want 2", len(got))
		}
	})
}
