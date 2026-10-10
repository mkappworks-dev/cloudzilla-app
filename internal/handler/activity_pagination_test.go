package handler_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedEvents gives the repo's owner n star events, newest first, shown as "<owner>/ev00", "<owner>/ev01" and so on.
func seedEvents(t *testing.T, repo seededRepo, n int) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	testutil.Exec(t, db,
		`INSERT INTO events (actor_id, actor_name, repo_id, repo_name, owner_name, event_type, created_at)
		 SELECT $1, $2, $3, 'ev' || lpad(i::text, 2, '0'), $2, 'star', NOW() - i * INTERVAL '1 minute'
		 FROM generate_series(0, $4::int - 1) AS i`,
		repo.owner.id, repo.owner.name, repo.id, n)
}

// shownEvents lists the "evNN" events rendered in body, once each: a row names its repo twice.
func shownEvents(body string) []string {
	return slices.Compact(regexp.MustCompile(`ev\d\d`).FindAllString(body, -1))
}

func eventRange(from, to int) []string {
	var out []string
	for i := from; i < to; i++ {
		out = append(out, fmt.Sprintf("ev%02d", i))
	}
	return out
}

func TestPageActivity_PagesTenEventsAtATimeAndKeepsTheScope(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := seedOwnedRepo(t, db, false)
	seedEvents(t, repo, 25)
	h := newPageHandler(t, db)
	page := func(n int) string {
		return getActivity(t, h, repo.owner.token, fmt.Sprintf("/activity?filter=yours&page=%d", n)).Body.String()
	}
	next := func(n int) string { return fmt.Sprintf(`<a href="/activity?filter=yours&amp;page=%d"`, n) }
	prevLink := `<a href="/activity?filter=yours"`

	first := page(1)
	if got, want := shownEvents(first), eventRange(0, 10); !slices.Equal(got, want) {
		t.Errorf("page 1 shows %v; want %v", got, want)
	}
	if !strings.Contains(first, next(2)) || strings.Contains(first, "&larr; Previous</a>") {
		t.Error("page 1: want a Next link to page 2 and no Previous link")
	}

	second := page(2)
	if got, want := shownEvents(second), eventRange(10, 20); !slices.Equal(got, want) {
		t.Errorf("page 2 shows %v; want %v (each event on exactly one page)", got, want)
	}
	if !strings.Contains(second, next(3)) || !strings.Contains(second, prevLink) || !strings.Contains(second, "&larr; Previous</a>") {
		t.Error("page 2: want Previous (page 1 omitted from the URL) and Next to page 3")
	}

	third := page(3)
	if got, want := shownEvents(third), eventRange(20, 25); !slices.Equal(got, want) {
		t.Errorf("page 3 shows %v; want %v", got, want)
	}
	if strings.Contains(third, "Next &rarr;</a>") {
		t.Error("page 3 holds the last event, so there is no Next link")
	}
}
