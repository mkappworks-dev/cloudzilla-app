package store_test

import (
	"context"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// Each repo holds a private issue (#1, open) and a public one (#2, closed),
// both pinned and in milestone #1. As in TestPrivateIssues_VisibleToRepoOwners,
// owners hold no permissions row.
func TestPrivateIssues_PinnedAndMilestoneLists(t *testing.T) {
	db := openTestDB(t)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	ownerName := "listvisowner_" + suffix
	ownerID := testutil.SeedUser(t, db, ownerName)
	orgOwnerID := testutil.SeedUser(t, db, "listvisorgowner_"+suffix)
	memberID := testutil.SeedUser(t, db, "listvismember_"+suffix)
	writerID := testutil.SeedUser(t, db, "listviswriter_"+suffix)
	readerID := testutil.SeedUser(t, db, "listvisreader_"+suffix)
	strangerID := testutil.SeedUser(t, db, "listvisstranger_"+suffix)

	var personal int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, $2, $3) RETURNING id`,
		ownerID, ownerName, "personal_"+suffix,
	).Scan(&personal); err != nil {
		t.Fatalf("insert personal repo: %v", err)
	}

	orgName := "listvisorg_" + suffix
	var orgID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING id`, orgName).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	testutil.Exec(t, db,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`,
		orgID, orgOwnerID, memberID)
	var orgRepo int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_name, org_id, created_by, name) VALUES ($1, $2, $3, $4) RETURNING id`,
		orgName, orgID, memberID, "orgrepo_"+suffix,
	).Scan(&orgRepo); err != nil {
		t.Fatalf("insert org repo: %v", err)
	}

	milestoneOf := map[int64]int64{}
	for _, repoID := range []int64{personal, orgRepo} {
		testutil.Exec(t, db,
			`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $3, 'writer'), ($2, $3, 'reader')`,
			writerID, readerID, repoID)
		var milestoneID int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO milestones (repo_id, number, title) VALUES ($1, 1, 'v1') RETURNING id`, repoID,
		).Scan(&milestoneID); err != nil {
			t.Fatalf("insert milestone: %v", err)
		}
		milestoneOf[repoID] = milestoneID
		testutil.Exec(t, db,
			`INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility, is_pinned, milestone_id)
			 VALUES ($1, 1, $2, 'private issue', '', 'open', 'private', TRUE, $3),
			        ($1, 2, $2, 'public issue', '', 'closed', 'public', TRUE, $3)`,
			repoID, writerID, milestoneID)
	}

	cases := []struct {
		repo   string
		repoID int64
		viewer string
		userID *int64
		want   bool
	}{
		{"personal", personal, "owner", &ownerID, true},
		{"personal", personal, "author", &writerID, true},
		{"personal", personal, "reader", &readerID, false},
		{"personal", personal, "stranger", &strangerID, false},
		{"personal", personal, "anonymous", nil, false},
		{"org", orgRepo, "org owner", &orgOwnerID, true},
		{"org", orgRepo, "author", &writerID, true},
		{"org", orgRepo, "reader", &readerID, false},
		{"org", orgRepo, "org member", &memberID, false},
		{"org", orgRepo, "anonymous", nil, false},
	}
	issues := store.NewIssueStore(db)
	milestones := store.NewMilestoneStore(db)
	for _, c := range cases {
		t.Run(c.repo+"/"+c.viewer, func(t *testing.T) {
			wantPinned, wantOpen, wantOpenCount := []int{2}, []int{}, 0
			if c.want {
				wantPinned, wantOpen, wantOpenCount = []int{1, 2}, []int{1}, 1
			}

			pinned, err := issues.ListPinned(ctx, c.repoID, c.userID)
			if err != nil {
				t.Fatalf("ListPinned: %v", err)
			}
			if got := issueNumbers(pinned); !slices.Equal(got, wantPinned) {
				t.Errorf("ListPinned = %v, want %v", got, wantPinned)
			}

			open, err := milestones.ListIssuesPaged(ctx, milestoneOf[c.repoID], "open", c.userID, 1, 50)
			if err != nil {
				t.Fatalf("ListIssuesPaged: %v", err)
			}
			if got := issueNumbers(open); !slices.Equal(got, wantOpen) {
				t.Errorf("ListIssuesPaged(open) = %v, want %v", got, wantOpen)
			}

			byNumber, err := milestones.GetByNumber(ctx, c.repoID, 1, c.userID)
			if err != nil {
				t.Fatalf("GetByNumber: %v", err)
			}
			byID, err := milestones.GetByID(ctx, milestoneOf[c.repoID], c.userID)
			if err != nil {
				t.Fatalf("GetByID: %v", err)
			}
			listed, err := milestones.ListByRepo(ctx, c.repoID, c.userID)
			if err != nil || len(listed) != 1 {
				t.Fatalf("ListByRepo = %d milestones, %v; want 1", len(listed), err)
			}
			for name, m := range map[string]*model.Milestone{"GetByNumber": byNumber, "GetByID": byID, "ListByRepo": &listed[0]} {
				if m.OpenCount != wantOpenCount || m.ClosedCount != 1 {
					t.Errorf("%s counts = %d open, %d closed; want %d open, 1 closed", name, m.OpenCount, m.ClosedCount, wantOpenCount)
				}
			}
		})
	}
}

func issueNumbers(issues []model.Issue) []int {
	numbers := []int{}
	for _, i := range issues {
		numbers = append(numbers, i.Number)
	}
	return numbers
}
