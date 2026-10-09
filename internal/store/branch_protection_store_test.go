package store_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestBranchProtectionStore_MatchForBranchSkipsMalformedPatternAndLogsOnce(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "owner"+suffix, suffix)
	s := store.NewBranchProtectionStore(db)
	ctx := context.Background()

	// The service refuses this pattern now, but rows stored before it did still exist.
	bad := "[" + suffix
	for _, p := range []string{bad, "release/*"} {
		if err := s.Create(ctx, &model.BranchProtection{RepoID: repoID, Pattern: p}); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	for range 3 {
		rule, err := s.MatchForBranch(ctx, repoID, "release/1.0")
		if err != nil {
			t.Fatal(err)
		}
		if rule == nil || rule.Pattern != "release/*" {
			t.Fatalf("rule = %+v, want the release/* rule despite the malformed one", rule)
		}
	}

	if n := strings.Count(buf.String(), bad); n != 1 {
		t.Errorf("malformed pattern logged %d times, want 1:\n%s", n, buf.String())
	}
}
