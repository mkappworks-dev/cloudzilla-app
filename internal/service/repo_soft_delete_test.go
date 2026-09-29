package service

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSoftDelete_SetsDeletedAt(t *testing.T) {
	// deleteGuard must allow the owner (isOwner = true) — no error expected.
	err := deleteGuard(true)
	if err != nil {
		t.Errorf("expected nil error for owner, got: %v", err)
	}
}

func TestSoftDelete_ForbiddenForNonOwner(t *testing.T) {
	// deleteGuard must reject a non-owner (isOwner = false).
	err := deleteGuard(false)
	if err == nil {
		t.Error("expected error for non-owner, got nil")
	}
}

// seedOwner inserts a user directly, so legacy names the owner-name rule now
// refuses can still be tested.
func seedOwner(t *testing.T, db *sql.DB, username string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		username, "owner_"+testutil.UniqueSuffix(t)+"@test.invalid",
	).Scan(&id); err != nil {
		t.Fatalf("seed owner %q: %v", username, err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, id) })
	return id
}

func seedRepoRow(t *testing.T, db *sql.DB, ownerID int64, ownerName, name, deletedAt string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch, deleted_at)
		 VALUES ($1, $2, $3, '', false, 'main', `+deletedAt+`) RETURNING id`,
		ownerID, ownerName, name,
	).Scan(&id); err != nil {
		t.Fatalf("seed repo %s/%s: %v", ownerName, name, err)
	}
	return id
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func newDiskRepoService(db *sql.DB, root string) *RepoService {
	return NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, config.GitConfig{ReposRoot: root})
}

func TestRestore_GlobOwnerNameLeavesOtherOwnersAlone(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	suffix := testutil.UniqueSuffix(t)
	attacker, victim := "*_"+suffix, "victim_"+suffix
	attackerID := seedOwner(t, db, attacker)
	victimID := seedOwner(t, db, victim)
	repoID := seedRepoRow(t, db, attackerID, attacker, "secret", "NOW()")
	seedRepoRow(t, db, victimID, victim, "secret", "NOW()")
	victimDeleted := filepath.Join(root, victim, "secret.git.deleted.1700000000")
	mkdirs(t, victimDeleted, filepath.Join(root, attacker))

	_ = newDiskRepoService(db, root).Restore(context.Background(), repoID, attackerID, false)

	if _, err := os.Stat(victimDeleted); err != nil {
		t.Errorf("the victim's soft-deleted repo must stay put: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, attacker, "secret.git")); !os.IsNotExist(err) {
		t.Errorf("nothing may be restored into the glob-named owner's dir; stat: %v", err)
	}
}

func TestRestore_LeavesLiveRepoNamedLikeADeletedDir(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner := "restorer_" + testutil.UniqueSuffix(t)
	ownerID := seedOwner(t, db, owner)
	repoID := seedRepoRow(t, db, ownerID, owner, "x", "NOW()")
	seedRepoRow(t, db, ownerID, owner, "x.git.deleted.1", "NULL")
	live := filepath.Join(root, owner, "x.git.deleted.1.git")
	mkdirs(t, live)

	if err := newDiskRepoService(db, root).Restore(context.Background(), repoID, ownerID, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, err := os.Stat(live); err != nil {
		t.Errorf("the live repo x.git.deleted.1 must stay put: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, owner, "x.git")); !os.IsNotExist(err) {
		t.Errorf("x has no soft-deleted dir, so nothing may appear at x.git; stat: %v", err)
	}
}

func TestRestore_MovesTheLatestDeletedDirBack(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner := "restorer_" + testutil.UniqueSuffix(t)
	ownerID := seedOwner(t, db, owner)
	repoID := seedRepoRow(t, db, ownerID, owner, "x", "NOW()")
	older := filepath.Join(root, owner, "x.git.deleted.1600000000")
	latest := filepath.Join(root, owner, "x.git.deleted.1700000000")
	mkdirs(t, older, filepath.Join(latest, "latest-marker"))

	if err := newDiskRepoService(db, root).Restore(context.Background(), repoID, ownerID, false); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, owner, "x.git", "latest-marker")); err != nil {
		t.Errorf("the latest soft-deleted dir must be restored to x.git: %v", err)
	}
	if _, err := os.Stat(older); err != nil {
		t.Errorf("older soft-deleted dirs stay put: %v", err)
	}
}

func TestPurgeExpired_LeavesLiveRepoNamedLikeADeletedDir(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner := "purger_" + testutil.UniqueSuffix(t)
	ownerID := seedOwner(t, db, owner)
	seedRepoRow(t, db, ownerID, owner, "x", "NOW() - INTERVAL '31 days'")
	seedRepoRow(t, db, ownerID, owner, "x.git.deleted.1", "NULL")
	live := filepath.Join(root, owner, "x.git.deleted.1.git")
	expired := filepath.Join(root, owner, "x.git.deleted.1600000000")
	mkdirs(t, live, expired)

	if err := newDiskRepoService(db, root).PurgeExpired(context.Background()); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}

	if _, err := os.Stat(live); err != nil {
		t.Errorf("the live repo x.git.deleted.1 must survive the purge of x: %v", err)
	}
	if _, err := os.Stat(expired); !os.IsNotExist(err) {
		t.Errorf("x's soft-deleted dir must be purged; stat: %v", err)
	}
}

// An owner stored before the owner-name rule can fail RepoDir. Its repos must
// still be deletable, so the UI can clean them up.
func TestDelete_LegacyUnsafeOwner_SoftDeletesRowAndWarns(t *testing.T) {
	db := testutil.OpenTestDB(t)
	owner := "*_" + testutil.UniqueSuffix(t)
	ownerID := seedOwner(t, db, owner)
	repoID := seedRepoRow(t, db, ownerID, owner, "legacy", "NULL")
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if err := newDiskRepoService(db, t.TempDir()).Delete(context.Background(), repoID, ownerID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var deleted bool
	if err := db.QueryRowContext(context.Background(), `SELECT deleted_at IS NOT NULL FROM repositories WHERE id = $1`, repoID).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("the repository row must be soft-deleted")
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "unsafe repo path") {
		t.Errorf("want a WARN about the unsafe repo path; logs:\n%s", logs.String())
	}
}
