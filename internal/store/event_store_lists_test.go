package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type eventFixture struct {
	db         *sql.DB
	es         *store.EventStore
	aliceID    int64
	bobID      int64
	alicePub   int64
	alicePriv  int64
	bobPub     int64
	bobPrivate int64
}

func newEventFixture(t *testing.T) eventFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	f := eventFixture{db: db, es: store.NewEventStore(db)}
	f.aliceID = testutil.SeedUser(t, db, "evla"+suffix)
	f.bobID = testutil.SeedUser(t, db, "evlb"+suffix)
	mk := func(owner int64, name string, private bool) int64 {
		var id int64
		if err := db.QueryRow(
			`INSERT INTO repositories (owner_id, owner_name, name, private) VALUES ($1, 'o', $2, $3) RETURNING id`,
			owner, name+suffix, private).Scan(&id); err != nil {
			t.Fatalf("insert repo: %v", err)
		}
		return id
	}
	f.alicePub = mk(f.aliceID, "apub", false)
	f.alicePriv = mk(f.aliceID, "apriv", true)
	f.bobPub = mk(f.bobID, "bpub", false)
	f.bobPrivate = mk(f.bobID, "bpriv", true)
	return f
}

// record inserts an event created age ago, so ordering does not depend on insert timing.
func (f eventFixture) record(t *testing.T, actor int64, repo *int64, typ string, age time.Duration) int64 {
	t.Helper()
	e := &model.Event{ActorID: actor, RepoID: repo, EventType: typ}
	if err := f.es.Record(context.Background(), e); err != nil {
		t.Fatalf("Record: %v", err)
	}
	testutil.Exec(t, f.db, `UPDATE events SET created_at = NOW() - make_interval(secs => $2) WHERE id = $1`, e.ID, age.Seconds())
	return e.ID
}

func eventIDs(evs []model.Event) []int64 {
	ids := make([]int64, len(evs))
	for i, e := range evs {
		ids[i] = e.ID
	}
	return ids
}

func sameIDs(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestEventStore_RecordDefaultsPayloadAndRepo(t *testing.T) {
	f := newEventFixture(t)
	ctx := context.Background()
	id := f.record(t, f.aliceID, nil, model.EventPush, 0)
	evs, err := f.es.ListOwnActivity(ctx, f.aliceID, 10, 0)
	if err != nil || len(evs) != 1 || evs[0].ID != id {
		t.Fatalf("ListOwnActivity = %+v, %v", evs, err)
	}
	if string(evs[0].Payload) != "{}" || evs[0].RepoID != nil {
		t.Errorf("payload = %q, repo = %v; want {} and nil", evs[0].Payload, evs[0].RepoID)
	}

	e := &model.Event{ActorID: f.aliceID, RepoID: &f.alicePub, EventType: model.EventPush, Payload: []byte(`{"n":1}`)}
	if err := f.es.Record(ctx, e); err != nil {
		t.Fatalf("Record: %v", err)
	}
	byRepo, _ := f.es.ListByRepo(ctx, f.alicePub, 1, 10)
	if len(byRepo) != 1 || byRepo[0].RepoID == nil || *byRepo[0].RepoID != f.alicePub || string(byRepo[0].Payload) != `{"n": 1}` {
		t.Errorf("ListByRepo = %+v", byRepo)
	}

	bad := &model.Event{ActorID: -1, EventType: model.EventPush}
	if err := f.es.Record(ctx, bad); err == nil {
		t.Error("Record with unknown actor must fail")
	}
}

func TestEventStore_ListByRepoOrdersNewestFirstAndPages(t *testing.T) {
	f := newEventFixture(t)
	ctx := context.Background()
	oldest := f.record(t, f.aliceID, &f.alicePub, model.EventPush, 30*time.Second)
	middle := f.record(t, f.bobID, &f.alicePub, model.EventPush, 20*time.Second)
	newest := f.record(t, f.aliceID, &f.alicePub, model.EventPush, 10*time.Second)
	f.record(t, f.aliceID, &f.bobPub, model.EventPush, 0)

	all, err := f.es.ListByRepo(ctx, f.alicePub, 1, 10)
	if err != nil || !sameIDs(eventIDs(all), []int64{newest, middle, oldest}) {
		t.Fatalf("ListByRepo = %v, %v", eventIDs(all), err)
	}
	p2, _ := f.es.ListByRepo(ctx, f.alicePub, 2, 2)
	if !sameIDs(eventIDs(p2), []int64{oldest}) {
		t.Errorf("page 2 = %v, want [oldest]", eventIDs(p2))
	}
	// A page below 1 clamps to the first row rather than erroring on a negative OFFSET.
	p0, err := f.es.ListByRepo(ctx, f.alicePub, 0, 2)
	if err != nil || !sameIDs(eventIDs(p0), []int64{newest, middle}) {
		t.Errorf("page 0 = %v, %v", eventIDs(p0), err)
	}
}

func TestEventStore_ListByActorHidesPrivateRepoEvents(t *testing.T) {
	f := newEventFixture(t)
	ctx := context.Background()
	noRepo := f.record(t, f.aliceID, nil, model.EventPush, 30*time.Second)
	pub := f.record(t, f.aliceID, &f.alicePub, model.EventPush, 20*time.Second)
	f.record(t, f.aliceID, &f.alicePriv, model.EventPush, 10*time.Second)
	f.record(t, f.bobID, &f.bobPub, model.EventPush, 0)

	got, err := f.es.ListByActor(ctx, f.aliceID, 1, 10)
	if err != nil || !sameIDs(eventIDs(got), []int64{pub, noRepo}) {
		t.Errorf("ListByActor = %v, %v; want public and repo-less events only", eventIDs(got), err)
	}
	own, _ := f.es.ListOwnActivity(ctx, f.aliceID, 10, 0)
	if len(own) != 3 {
		t.Errorf("ListOwnActivity = %d events, want all 3 including private", len(own))
	}
	if p, _ := f.es.ListOwnActivity(ctx, f.aliceID, 2, 2); len(p) != 1 {
		t.Errorf("ListOwnActivity page 2 = %d, want 1", len(p))
	}
	if p, _ := f.es.ListByActor(ctx, f.aliceID, -3, 1); len(p) != 1 {
		t.Errorf("ListByActor page -3 = %d, want 1", len(p))
	}
}

func TestEventStore_WatchingAndFeedScopes(t *testing.T) {
	f := newEventFixture(t)
	ctx := context.Background()

	watched := f.record(t, f.bobID, &f.bobPub, model.EventPush, 40*time.Second)
	f.record(t, f.bobID, &f.bobPrivate, model.EventPush, 30*time.Second) // watched but unreadable
	ignored := f.record(t, f.bobID, &f.alicePub, model.EventPush, 20*time.Second)
	owned := f.record(t, f.bobID, &f.alicePriv, model.EventPush, 10*time.Second)

	testutil.Exec(t, f.db, `INSERT INTO watches (user_id, repo_id, level) VALUES ($1, $2, 'watching')`, f.aliceID, f.bobPub)
	testutil.Exec(t, f.db, `INSERT INTO watches (user_id, repo_id, level) VALUES ($1, $2, 'watching')`, f.aliceID, f.bobPrivate)
	testutil.Exec(t, f.db, `INSERT INTO watches (user_id, repo_id, level) VALUES ($1, $2, 'ignoring')`, f.aliceID, f.alicePub)

	watching, err := f.es.ListWatching(ctx, f.aliceID, 10, 0)
	if err != nil || !sameIDs(eventIDs(watching), []int64{watched}) {
		t.Errorf("ListWatching = %v, %v; want only the readable watched repo", eventIDs(watching), err)
	}
	if p, _ := f.es.ListWatching(ctx, f.aliceID, 10, 0); len(p) != 1 {
		t.Errorf("ListWatching page 0 = %d, want 1", len(p))
	}

	// "ignoring" removes a repo from the watched half, yet owned repos still feed the user;
	// here alicePub is both ignored and owned, so ownership wins.
	feed, err := f.es.ListForFeed(ctx, f.aliceID, 10, 0)
	if err != nil || !sameIDs(eventIDs(feed), []int64{owned, ignored, watched}) {
		t.Errorf("ListForFeed = %v, %v; want [owned ignored watched]", eventIDs(feed), err)
	}
	if p, _ := f.es.ListForFeed(ctx, f.aliceID, 1, 0); !sameIDs(eventIDs(p), []int64{owned}) {
		t.Errorf("ListForFeed page 0 = %v", eventIDs(p))
	}
	if p, _ := f.es.ListForFeed(ctx, f.aliceID, 1, 1); !sameIDs(eventIDs(p), []int64{ignored}) {
		t.Errorf("ListForFeed page 2 = %v", eventIDs(p))
	}

	counts, err := f.es.FeedCounts(ctx, f.aliceID)
	if err != nil {
		t.Fatalf("FeedCounts: %v", err)
	}
	if counts["all"] != 3 || counts["watching"] != 1 || counts["yours"] != 0 {
		t.Errorf("FeedCounts = %v, want all=3 watching=1 yours=0", counts)
	}
}

func TestEventStore_FeedIncludesInvolvedIssueOnUnwatchedRepo(t *testing.T) {
	f := newEventFixture(t)
	ctx := context.Background()
	testutil.Exec(t, f.db,
		`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, 7, $2, 't', '', 'open', 'public')`,
		f.bobPub, f.aliceID)
	involved := f.recordWithPayload(t, f.bobID, f.bobPub, model.EventIssueOpened, `{"number":7}`)
	f.recordWithPayload(t, f.bobID, f.bobPub, model.EventIssueOpened, `{"number":8}`)

	feed, err := f.es.ListForFeed(ctx, f.aliceID, 10, 0)
	if err != nil || !sameIDs(eventIDs(feed), []int64{involved}) {
		t.Errorf("ListForFeed = %v, %v; want only the authored issue's event", eventIDs(feed), err)
	}
}

func (f eventFixture) recordWithPayload(t *testing.T, actor, repo int64, typ, payload string) int64 {
	t.Helper()
	e := &model.Event{ActorID: actor, RepoID: &repo, EventType: typ, Payload: []byte(payload)}
	if err := f.es.Record(context.Background(), e); err != nil {
		t.Fatalf("Record: %v", err)
	}
	return e.ID
}
