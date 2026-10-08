package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type discussionFixture struct {
	s        *store.DiscussionStore
	repoID   int64
	authorID int64
	general  int64
	qa       int64
}

func newDiscussionFixture(t *testing.T, db *sql.DB) discussionFixture {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)
	s := store.NewDiscussionStore(db)
	cats, err := s.ListCategories(context.Background())
	if err != nil || len(cats) < 2 {
		t.Fatalf("ListCategories = %v, %v; want the seeded categories", cats, err)
	}
	return discussionFixture{
		s:        s,
		repoID:   testutil.SeedRepo(t, db, authorID, "testuser_"+suffix, suffix),
		authorID: authorID,
		general:  cats[0].ID,
		qa:       cats[1].ID,
	}
}

func (f discussionFixture) discussion(t *testing.T, categoryID int64, title string) *model.Discussion {
	t.Helper()
	d := &model.Discussion{RepoID: f.repoID, CategoryID: categoryID, Title: title, Body: "body", AuthorID: f.authorID, AuthorName: "author"}
	if err := f.s.Create(context.Background(), d); err != nil {
		t.Fatalf("Create %q: %v", title, err)
	}
	return d
}

func (f discussionFixture) reply(t *testing.T, discussionID int64, parent sql.NullInt64, body string) *model.DiscussionReply {
	t.Helper()
	r := &model.DiscussionReply{DiscussionID: discussionID, ParentID: parent, AuthorID: f.authorID, AuthorName: "author", Body: body}
	if err := f.s.CreateReply(context.Background(), r); err != nil {
		t.Fatalf("CreateReply %q: %v", body, err)
	}
	return r
}

func TestDiscussionStore_Categories(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	s := store.NewDiscussionStore(db)

	cats, err := s.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if len(cats) < 4 {
		t.Fatalf("got %d categories; want the 4 seeded ones", len(cats))
	}
	for i := 1; i < len(cats); i++ {
		if cats[i].ID <= cats[i-1].ID {
			t.Errorf("categories not ordered by id: %d then %d", cats[i-1].ID, cats[i].ID)
		}
	}

	got, err := s.GetCategory(ctx, cats[0].ID)
	if err != nil || got == nil || *got != cats[0] {
		t.Errorf("GetCategory = %+v, %v; want %+v", got, err, cats[0])
	}
	if got, err := s.GetCategory(ctx, -1); got != nil || err != nil {
		t.Errorf("GetCategory unknown = %+v, %v; want nil, nil", got, err)
	}
}

func TestDiscussionStore_Create_NumbersPerRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	f := newDiscussionFixture(t, db)
	other := newDiscussionFixture(t, db)

	if n, err := f.s.NextNumber(ctx, f.repoID); err != nil || n != 1 {
		t.Fatalf("NextNumber on an empty repo = %d, %v; want 1", n, err)
	}
	first := f.discussion(t, f.general, "first")
	second := f.discussion(t, f.general, "second")
	otherFirst := other.discussion(t, other.general, "elsewhere")

	if first.Number != 1 || second.Number != 2 || otherFirst.Number != 1 {
		t.Errorf("numbers = %d, %d, %d; want 1, 2, 1", first.Number, second.Number, otherFirst.Number)
	}
	if first.ID == 0 || first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Errorf("Create must fill ID and timestamps: %+v", first)
	}
	if n, _ := f.s.NextNumber(ctx, f.repoID); n != 3 {
		t.Errorf("NextNumber = %d; want 3", n)
	}

	// A gap left by a deleted discussion is not reused while a higher number exists.
	testutil.Exec(t, db, `DELETE FROM discussions WHERE id = $1`, first.ID)
	if n, _ := f.s.NextNumber(ctx, f.repoID); n != 3 {
		t.Errorf("NextNumber after deleting #1 = %d; want 3", n)
	}
}

func TestDiscussionStore_Create_UnknownCategory(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newDiscussionFixture(t, db)
	d := &model.Discussion{RepoID: f.repoID, CategoryID: -1, Title: "t", AuthorID: f.authorID}
	if err := f.s.Create(context.Background(), d); !errors.Is(err, store.ErrUnknownDiscussionCategory) {
		t.Errorf("Create = %v; want ErrUnknownDiscussionCategory", err)
	}
}

func TestDiscussionStore_Create_OtherForeignKeysKeepTheirError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newDiscussionFixture(t, db)
	d := &model.Discussion{RepoID: f.repoID, CategoryID: f.general, Title: "t", AuthorID: -1}
	err := f.s.Create(context.Background(), d)
	if err == nil || errors.Is(err, store.ErrUnknownDiscussionCategory) {
		t.Errorf("Create with a missing author = %v; want the raw foreign-key error", err)
	}
}

func TestDiscussionStore_GetByNumber(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	f := newDiscussionFixture(t, db)
	d := f.discussion(t, f.qa, "title")

	got, err := f.s.GetByNumber(ctx, f.repoID, d.Number)
	if err != nil || got == nil {
		t.Fatalf("GetByNumber = %v, %v", got, err)
	}
	if got.ID != d.ID || got.CategoryID != f.qa || got.Title != "title" || got.Body != "body" ||
		got.AuthorID != f.authorID || got.AuthorName != "author" || got.IsLocked || got.IsAnswered || got.AnswerID.Valid {
		t.Errorf("GetByNumber = %+v", got)
	}
	if got, err := f.s.GetByNumber(ctx, f.repoID, 99); got != nil || err != nil {
		t.Errorf("unknown number = %v, %v; want nil, nil", got, err)
	}
	other := newDiscussionFixture(t, db)
	if got, err := other.s.GetByNumber(ctx, other.repoID, d.Number); got != nil || err != nil {
		t.Errorf("number from another repo = %v, %v; want nil, nil", got, err)
	}
}

func TestDiscussionStore_ListAndCount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	f := newDiscussionFixture(t, db)
	other := newDiscussionFixture(t, db)

	if ds, err := f.s.List(ctx, f.repoID, 0); err != nil || len(ds) != 0 {
		t.Fatalf("empty List = %v, %v", ds, err)
	}
	if n, err := f.s.CountByRepo(ctx, f.repoID); err != nil || n != 0 {
		t.Fatalf("empty CountByRepo = %d, %v", n, err)
	}
	a := f.discussion(t, f.general, "a")
	b := f.discussion(t, f.qa, "b")
	c := f.discussion(t, f.general, "c")
	other.discussion(t, other.general, "noise")
	// Ordered by created_at rather than id: b is created second but made the newest.
	testutil.Exec(t, db, `UPDATE discussions SET created_at = NOW() - INTERVAL '2 hours' WHERE id = $1`, a.ID)
	testutil.Exec(t, db, `UPDATE discussions SET created_at = NOW() + INTERVAL '1 hour' WHERE id = $1`, b.ID)
	testutil.Exec(t, db, `UPDATE discussions SET created_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, c.ID)

	tests := []struct {
		name       string
		categoryID int64
		want       []int64
	}{
		{"all categories, newest first", 0, []int64{b.ID, c.ID, a.ID}},
		{"one category", f.general, []int64{c.ID, a.ID}},
		{"category with none", -1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := f.s.List(ctx, f.repoID, tt.categoryID)
			if err != nil {
				t.Fatal(err)
			}
			var ids []int64
			for _, d := range ds {
				ids = append(ids, d.ID)
			}
			if len(ids) != len(tt.want) {
				t.Fatalf("ids = %v; want %v", ids, tt.want)
			}
			for i := range ids {
				if ids[i] != tt.want[i] {
					t.Fatalf("ids = %v; want %v", ids, tt.want)
				}
			}
		})
	}
	if n, err := f.s.CountByRepo(ctx, f.repoID); err != nil || n != 3 {
		t.Errorf("CountByRepo = %d, %v; want 3", n, err)
	}
}

func TestDiscussionStore_Mutations(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	f := newDiscussionFixture(t, db)
	d := f.discussion(t, f.general, "old")
	reload := func() *model.Discussion {
		t.Helper()
		got, err := f.s.GetByNumber(ctx, f.repoID, d.Number)
		if err != nil || got == nil {
			t.Fatalf("GetByNumber = %v, %v", got, err)
		}
		return got
	}

	if err := f.s.LockDiscussion(ctx, d.ID, true); err != nil {
		t.Fatal(err)
	}
	if !reload().IsLocked {
		t.Error("LockDiscussion(true) did not lock")
	}
	if err := f.s.LockDiscussion(ctx, d.ID, false); err != nil {
		t.Fatal(err)
	}
	if reload().IsLocked {
		t.Error("LockDiscussion(false) did not unlock")
	}

	if err := f.s.UpdateContent(ctx, d.ID, "new title", "new body"); err != nil {
		t.Fatal(err)
	}
	if got := reload(); got.Title != "new title" || got.Body != "new body" {
		t.Errorf("after UpdateContent: %q / %q", got.Title, got.Body)
	}

	if err := f.s.SetCategory(ctx, d.ID, f.qa); err != nil {
		t.Fatalf("SetCategory: %v", err)
	}
	if got := reload(); got.CategoryID != f.qa {
		t.Errorf("category = %d; want %d", got.CategoryID, f.qa)
	}
	if err := f.s.SetCategory(ctx, -1, f.qa); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("SetCategory unknown discussion = %v; want sql.ErrNoRows", err)
	}
	if err := f.s.SetCategory(ctx, d.ID, -1); err == nil {
		t.Error("SetCategory to an unknown category must fail")
	}
}

func TestDiscussionStore_Replies(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	f := newDiscussionFixture(t, db)
	d := f.discussion(t, f.general, "d")
	other := f.discussion(t, f.general, "other")

	if rs, err := f.s.ListReplies(ctx, d.ID); err != nil || len(rs) != 0 {
		t.Fatalf("empty ListReplies = %v, %v", rs, err)
	}
	top := f.reply(t, d.ID, sql.NullInt64{}, "top")
	child := f.reply(t, d.ID, sql.NullInt64{Int64: top.ID, Valid: true}, "child")
	f.reply(t, other.ID, sql.NullInt64{}, "elsewhere")
	if top.ID == 0 || top.CreatedAt.IsZero() {
		t.Errorf("CreateReply must fill ID and timestamps: %+v", top)
	}
	testutil.Exec(t, db, `UPDATE discussion_replies SET created_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, top.ID)

	rs, err := f.s.ListReplies(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].ID != top.ID || rs[1].ID != child.ID {
		t.Fatalf("ListReplies = %+v; want [top child] oldest first", rs)
	}
	if rs[0].ParentID.Valid || !rs[1].ParentID.Valid || rs[1].ParentID.Int64 != top.ID || rs[0].Body != "top" || rs[0].IsAnswer {
		t.Errorf("reply fields wrong: %+v", rs)
	}

	bad := &model.DiscussionReply{DiscussionID: d.ID, ParentID: sql.NullInt64{Int64: -1, Valid: true}, AuthorID: f.authorID, Body: "x"}
	if err := f.s.CreateReply(ctx, bad); err == nil {
		t.Error("CreateReply with a missing parent must fail")
	}

	if err := f.s.DeleteReply(ctx, top.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	if rs, _ := f.s.ListReplies(ctx, d.ID); len(rs) != 2 {
		t.Errorf("DeleteReply through another discussion removed a reply: %d left", len(rs))
	}
	if err := f.s.DeleteReply(ctx, top.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if rs, _ := f.s.ListReplies(ctx, d.ID); len(rs) != 0 {
		t.Errorf("deleting a parent must cascade to its children, %d left", len(rs))
	}
}

func TestDiscussionStore_SetAnswer(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	f := newDiscussionFixture(t, db)
	d := f.discussion(t, f.qa, "question")
	other := f.discussion(t, f.qa, "other question")
	r1 := f.reply(t, d.ID, sql.NullInt64{}, "r1")
	r2 := f.reply(t, d.ID, sql.NullInt64{}, "r2")
	foreign := f.reply(t, other.ID, sql.NullInt64{}, "foreign")

	state := func() (*model.Discussion, map[int64]bool) {
		t.Helper()
		got, err := f.s.GetByNumber(ctx, f.repoID, d.Number)
		if err != nil || got == nil {
			t.Fatalf("GetByNumber = %v, %v", got, err)
		}
		rs, err := f.s.ListReplies(ctx, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		flags := map[int64]bool{}
		for _, r := range rs {
			flags[r.ID] = r.IsAnswer
		}
		return got, flags
	}

	if err := f.s.SetAnswer(ctx, d.ID, &r1.ID); err != nil {
		t.Fatalf("SetAnswer r1: %v", err)
	}
	got, flags := state()
	if !got.IsAnswered || !got.AnswerID.Valid || got.AnswerID.Int64 != r1.ID || !flags[r1.ID] || flags[r2.ID] {
		t.Errorf("after r1: %+v %v", got, flags)
	}

	if err := f.s.SetAnswer(ctx, d.ID, &r2.ID); err != nil {
		t.Fatalf("SetAnswer r2: %v", err)
	}
	got, flags = state()
	if got.AnswerID.Int64 != r2.ID || flags[r1.ID] || !flags[r2.ID] {
		t.Errorf("switching the answer must move the flag: %+v %v", got, flags)
	}

	if err := f.s.SetAnswer(ctx, d.ID, &foreign.ID); err == nil {
		t.Error("SetAnswer with another discussion's reply must fail")
	}
	got, flags = state()
	if got.AnswerID.Int64 != r2.ID || !flags[r2.ID] {
		t.Errorf("a refused SetAnswer changed state: %+v %v", got, flags)
	}

	if err := f.s.SetAnswer(ctx, d.ID, nil); err != nil {
		t.Fatalf("SetAnswer nil: %v", err)
	}
	got, flags = state()
	if got.IsAnswered || got.AnswerID.Valid || flags[r1.ID] || flags[r2.ID] {
		t.Errorf("clearing the answer left %+v %v", got, flags)
	}
}

func TestDiscussionStore_DeletingTheAnswerReplyClearsAnswerID(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	f := newDiscussionFixture(t, db)
	d := f.discussion(t, f.qa, "question")
	r := f.reply(t, d.ID, sql.NullInt64{}, "r")
	if err := f.s.SetAnswer(ctx, d.ID, &r.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteReply(ctx, r.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.GetByNumber(ctx, f.repoID, d.Number)
	if err != nil || got == nil {
		t.Fatalf("GetByNumber = %v, %v", got, err)
	}
	if got.AnswerID.Valid {
		t.Errorf("answer_id = %v; want NULL after the reply is deleted", got.AnswerID)
	}
}

func TestDiscussionStore_ClosedDBErrors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := store.NewDiscussionStore(db)
	one := int64(1)

	checks := map[string]error{}
	_, checks["ListCategories"] = s.ListCategories(ctx)
	_, checks["GetCategory"] = s.GetCategory(ctx, 1)
	_, checks["NextNumber"] = s.NextNumber(ctx, 1)
	checks["Create"] = s.Create(ctx, &model.Discussion{})
	_, checks["List all"] = s.List(ctx, 1, 0)
	_, checks["List category"] = s.List(ctx, 1, 1)
	_, checks["CountByRepo"] = s.CountByRepo(ctx, 1)
	_, checks["GetByNumber"] = s.GetByNumber(ctx, 1, 1)
	checks["SetAnswer nil"] = s.SetAnswer(ctx, 1, nil)
	checks["SetAnswer reply"] = s.SetAnswer(ctx, 1, &one)
	checks["LockDiscussion"] = s.LockDiscussion(ctx, 1, true)
	checks["UpdateContent"] = s.UpdateContent(ctx, 1, "t", "b")
	checks["SetCategory"] = s.SetCategory(ctx, 1, 1)
	checks["CreateReply"] = s.CreateReply(ctx, &model.DiscussionReply{})
	_, checks["ListReplies"] = s.ListReplies(ctx, 1)
	checks["DeleteReply"] = s.DeleteReply(ctx, 1, 1)
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s on a closed db must fail", name)
		}
	}
}
