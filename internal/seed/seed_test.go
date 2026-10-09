package seed_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/seed"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var testNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func newInstance(t *testing.T) (*service.Services, *sql.DB, string) {
	t.Helper()
	db := testutil.OpenFreshTestDB(t)
	root := t.TempDir()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: "seed-test-secret-0123456789abcdef", JWTExpiry: time.Hour, CookieName: "cz_seed_test"},
		Git:  config.GitConfig{ReposRoot: root},
	}
	return service.New(store.New(db), cfg), db, root
}

func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func smallWorld(seedValue uint64) seed.Options {
	return seed.Options{Users: 6, Orgs: 2, Repos: 5, Seed: seedValue, Now: testNow}
}

func TestRun_SeedsAFreshInstance(t *testing.T) {
	svcs, db, root := newInstance(t)
	ctx := context.Background()

	rep, err := seed.Run(ctx, svcs, root, smallWorld(7))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, c := range []struct {
		query string
		want  int
	}{
		{`SELECT count(*) FROM users WHERE username <> 'ghost'`, rep.Users},
		{`SELECT count(*) FROM organizations`, rep.Orgs},
		{`SELECT count(*) FROM repositories`, rep.Repos},
		{`SELECT count(*) FROM issues`, rep.Issues},
		{`SELECT count(*) FROM pull_requests`, rep.Pulls},
		{`SELECT count(*) FROM discussions`, rep.Discussions},
		{`SELECT count(*) FROM releases`, rep.Releases},
		{`SELECT count(*) FROM stars`, rep.Stars},
		{`SELECT count(*) FROM gists`, rep.Gists},
	} {
		if got := count(t, db, c.query); got != c.want {
			t.Errorf("%s = %d, report says %d", c.query, got, c.want)
		}
	}
	if rep.Users != 7 || rep.Orgs != 2 || rep.Repos != 5 {
		t.Errorf("report = %+v, want 7 users (admin included), 2 orgs, 5 repos", rep)
	}
	if rep.Pulls == 0 || count(t, db, `SELECT count(*) FROM pull_requests WHERE state = 'merged'`) == 0 {
		t.Errorf("want merged pull requests, report %+v", rep)
	}

	if _, _, err := svcs.User.Authenticate(ctx, seed.AdminEmail, seed.DefaultPassword); err != nil {
		t.Errorf("admin cannot sign in with the default password: %v", err)
	}
	if n := count(t, db, `SELECT count(*) FROM users WHERE is_superadmin`); n != 1 {
		t.Errorf("%d superadmins, want 1", n)
	}

	// Post-receive ran on the backdated history, so the heatmap reaches back past the last month.
	old := count(t, db, `SELECT count(*) FROM commit_day_counts WHERE day < '2026-09-01'`)
	if old == 0 {
		t.Error("no commit_day_counts before September: history was not backdated or not ingested")
	}
	if n := count(t, db, `SELECT count(*) FROM events WHERE event_type = 'push'`); n == 0 {
		t.Error("no push events recorded")
	}
}

func TestRun_SeedsEveryShapeTheUIRenders(t *testing.T) {
	svcs, db, root := newInstance(t)
	// Milestones scale with repo popularity, which is skewed low, so five repos rarely carry every shape.
	rep, err := seed.Run(context.Background(), svcs, root, seed.Options{Users: 8, Orgs: 2, Repos: 14, Seed: 7, Now: testNow})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, c := range []struct{ name, query string }{
		{"issue open", `SELECT count(*) FROM issues WHERE state = 'open'`},
		{"issue closed", `SELECT count(*) FROM issues WHERE state = 'closed'`},
		{"pull open", `SELECT count(*) FROM pull_requests WHERE state = 'open' AND NOT is_draft`},
		{"pull draft", `SELECT count(*) FROM pull_requests WHERE state = 'open' AND is_draft`},
		{"pull merged", `SELECT count(*) FROM pull_requests WHERE state = 'merged'`},
		{"pull closed", `SELECT count(*) FROM pull_requests WHERE state = 'closed'`},
		{"pull in a milestone", `SELECT count(*) FROM pull_requests WHERE milestone_id IS NOT NULL`},
		{"release published", `SELECT count(*) FROM releases WHERE NOT is_draft AND NOT is_prerelease`},
		{"release prerelease", `SELECT count(*) FROM releases WHERE NOT is_draft AND is_prerelease`},
		{"release draft", `SELECT count(*) FROM releases WHERE is_draft`},
		{"open milestone with issues", `SELECT count(DISTINCT m.id) FROM milestones m JOIN issues i ON i.milestone_id = m.id WHERE m.state = 'open'`},
		{"closed milestone with issues", `SELECT count(DISTINCT m.id) FROM milestones m JOIN issues i ON i.milestone_id = m.id WHERE m.state = 'closed'`},
		{"milestone due in the past", `SELECT count(*) FROM milestones WHERE due_date < '2026-10-01'`},
		{"milestone due in the future", `SELECT count(*) FROM milestones WHERE due_date > '2026-10-01'`},
		{"issue with labels and assignee", `SELECT count(*) FROM issues i WHERE EXISTS (SELECT 1 FROM issue_labels l WHERE l.issue_id = i.id) AND EXISTS (SELECT 1 FROM issue_assignees a WHERE a.issue_id = i.id)`},
		{"board with three columns", `SELECT count(*) FROM (SELECT project_id FROM project_columns GROUP BY project_id HAVING count(*) >= 3) b`},
		{"closed board", `SELECT count(*) FROM projects WHERE closed_at IS NOT NULL`},
		{"open board", `SELECT count(*) FROM projects WHERE closed_at IS NULL`},
		{"note with description and due date", `SELECT count(*) FROM project_cards WHERE title <> '' AND note <> '' AND due_date IS NOT NULL AND issue_id IS NULL AND pull_id IS NULL`},
		{"note with assignees and labels", `SELECT count(*) FROM project_cards c WHERE c.title <> '' AND EXISTS (SELECT 1 FROM card_assignees a WHERE a.card_id = c.id) AND EXISTS (SELECT 1 FROM card_labels l WHERE l.card_id = c.id)`},
		{"titled note linked to an issue", `SELECT count(*) FROM project_cards WHERE title <> '' AND issue_id IS NOT NULL`},
		{"plain issue card", `SELECT count(*) FROM project_cards WHERE title = '' AND issue_id IS NOT NULL`},
		{"plain pull card", `SELECT count(*) FROM project_cards WHERE title = '' AND pull_id IS NOT NULL`},
		{"overdue card", `SELECT count(*) FROM project_cards WHERE due_date < '2026-10-01'`},
		{"card referencing a seeded issue", `SELECT count(*) FROM project_cards c JOIN project_columns pc ON pc.id = c.column_id JOIN projects p ON p.id = pc.project_id JOIN issues i ON i.repo_id = p.repo_id AND c.note LIKE '%#' || i.number || '%'`},
	} {
		if got := count(t, db, c.query); got == 0 {
			t.Errorf("no %s", c.name)
		}
	}

	if got := count(t, db, `SELECT count(*) FROM projects`); got != rep.Projects {
		t.Errorf("projects = %d, report says %d", got, rep.Projects)
	}
	if got := count(t, db, `SELECT count(*) FROM project_cards`); got != rep.Cards {
		t.Errorf("cards = %d, report says %d", got, rep.Cards)
	}
	if got := count(t, db, `SELECT count(*) FROM issues`); got != rep.Issues {
		t.Errorf("issues = %d, report says %d: a converted card creates an issue the report must count", got, rep.Issues)
	}
}

func TestRun_RefusesAnInstanceThatAlreadyHasAccounts(t *testing.T) {
	svcs, db, root := newInstance(t)
	ctx := context.Background()
	if _, err := seed.Run(ctx, svcs, root, seed.Options{Users: 1, Repos: 1, Seed: 1, Now: testNow}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	before := count(t, db, `SELECT count(*) FROM users`)

	_, err := seed.Run(ctx, svcs, t.TempDir(), seed.Options{Users: 1, Repos: 1, Seed: 2, Now: testNow})
	if !errors.Is(err, seed.ErrNotFresh) {
		t.Fatalf("second Run err = %v, want ErrNotFresh", err)
	}
	if after := count(t, db, `SELECT count(*) FROM users`); after != before {
		t.Errorf("refused Run still created users: %d -> %d", before, after)
	}
}

func TestRun_RefusesANonEmptyReposRoot(t *testing.T) {
	svcs, db, root := newInstance(t)
	if err := os.WriteFile(filepath.Join(root, "leftover"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := seed.Run(context.Background(), svcs, root, smallWorld(1))
	if !errors.Is(err, seed.ErrNotFresh) {
		t.Fatalf("err = %v, want ErrNotFresh", err)
	}
	if n := count(t, db, `SELECT count(*) FROM users WHERE username <> 'ghost'`); n != 0 {
		t.Errorf("refused Run created %d users", n)
	}
}

func TestRun_SameSeedBuildsTheSameWorld(t *testing.T) {
	names := func() []string {
		svcs, db, root := newInstance(t)
		if _, err := seed.Run(context.Background(), svcs, root, smallWorld(9)); err != nil {
			t.Fatalf("Run: %v", err)
		}
		rows, err := db.Query(`SELECT owner_name || '/' || name FROM repositories ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			out = append(out, n)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if a, b := names(), names(); !slices.Equal(a, b) {
		t.Errorf("repositories differ between runs:\n%v\n%v", a, b)
	}
}
