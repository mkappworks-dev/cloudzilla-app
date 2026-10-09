package db_test

import (
	"os"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const subjectKindMigration = "migrations/114_notification_subject_kind.sql"

func TestSubjectKindMigration_BackfillsFromTypeAndMentionURL(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	owner := seedNamedUser(t, db, "alice")
	var repoID int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'alice', 'mine') RETURNING id`, owner).Scan(&repoID); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	cases := []struct {
		typ, url string
		want     *string
	}{
		{"issue_comment", "/alice/mine/issues/1", ptr("issue")},
		{"issue_closed", "/alice/mine/issues/1", ptr("issue")},
		{"issue_reopened", "/alice/mine/issues/1", ptr("issue")},
		{"pr_comment", "/alice/mine/pulls/2", ptr("pull")},
		{"pr_merged", "/alice/mine/pulls/2", ptr("pull")},
		{"pr_closed", "/alice/mine/pulls/2", ptr("pull")},
		{"pr_opened", "/alice/mine/pulls/2", ptr("pull")},
		{"pr_review", "/alice/mine/pulls/2", ptr("pull")},
		{"discussion_reply", "/alice/mine/discussions/3", ptr("discussion")},
		{"mention", "/alice/mine/issues/1", ptr("issue")},
		{"mention", "/alice/mine/pulls/2", ptr("pull")},
		{"mention", "/alice/mine/discussions/3", ptr("discussion")},
		{"mention", "/x", nil},
		{"repo_transfer", "/repos/transfers", nil},
	}
	ids := make([]int64, len(cases))
	for i, c := range cases {
		if err := db.QueryRow(
			`INSERT INTO notifications (user_id, actor_id, actor_name, type, repo_id, repo_name, owner_name, subject_id, subject_url)
			 VALUES ($1, $1, 'a', $2, $3, 'mine', 'alice', 1, $4) RETURNING id`,
			owner, c.typ, repoID, c.url,
		).Scan(&ids[i]); err != nil {
			t.Fatalf("seed %s: %v", c.typ, err)
		}
	}
	testutil.Exec(t, db, `UPDATE notifications SET subject_kind = NULL`)
	migration, err := os.ReadFile(subjectKindMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if _, err := db.Exec(string(migration)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		for i, c := range cases {
			var got *string
			if err := db.QueryRow(`SELECT subject_kind FROM notifications WHERE id = $1`, ids[i]).Scan(&got); err != nil {
				t.Fatalf("read %s: %v", c.typ, err)
			}
			if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
				t.Errorf("run %d: %s %s: subject_kind = %v, want %v", run, c.typ, c.url, deref(got), deref(c.want))
			}
		}
	}
}

func ptr(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return "NULL"
	}
	return *s
}
