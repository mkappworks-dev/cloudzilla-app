package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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

// sameNameOwners returns one owner name and two owner IDs. UNIQUE(owner_id, name)
// spans soft-deleted rows, so only another owner ID can re-create x under the
// same owner name, as in an org, whose repos record the creating member.
func sameNameOwners(t *testing.T, db *sql.DB) (owner string, first, second int64) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	return "acme_" + suffix, seedOwner(t, db, "member1_"+suffix), seedOwner(t, db, "member2_"+suffix)
}

func deleteRepo(t *testing.T, svc *RepoService, repoID, userID int64) {
	t.Helper()
	if err := svc.Delete(context.Background(), repoID, userID); err != nil {
		t.Fatalf("Delete %d: %v", repoID, err)
	}
}

func restoreRepo(t *testing.T, svc *RepoService, repoID, userID int64) {
	t.Helper()
	if err := svc.Restore(context.Background(), repoID, userID, false); err != nil {
		t.Fatalf("Restore %d: %v", repoID, err)
	}
}

func purgeExpired(t *testing.T, svc *RepoService) {
	t.Helper()
	if err := svc.PurgeExpired(context.Background()); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
}

func assertExists(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("want %s: %v", p, err)
		}
	}
}

func assertMissing(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("want no %s; stat: %v", p, err)
		}
	}
}

func assertDirHolds(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s holds %v, want %v", dir, got, want)
	}
}

func TestDelete_MovesTheWikiAndRestoreBringsItBack(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner := "wikimover_" + testutil.UniqueSuffix(t)
	ownerID := seedOwner(t, db, owner)
	repoID := seedRepoRow(t, db, ownerID, owner, "x", "NULL")
	repo, wiki := filepath.Join(root, owner, "x.git"), filepath.Join(root, owner, "x.wiki.git")
	mkdirs(t, filepath.Join(repo, "objects"), filepath.Join(wiki, "page"))
	svc := newDiskRepoService(db, root)

	deleteRepo(t, svc, repoID, ownerID)
	assertMissing(t, repo, wiki)

	restoreRepo(t, svc, repoID, ownerID)
	assertExists(t, filepath.Join(repo, "objects"), filepath.Join(wiki, "page"))
}

func TestPurgeExpired_RemovesTheRepoAndItsWiki(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner := "wikipurger_" + testutil.UniqueSuffix(t)
	ownerID := seedOwner(t, db, owner)
	repoID := seedRepoRow(t, db, ownerID, owner, "x", "NULL")
	mkdirs(t, filepath.Join(root, owner, "x.git"), filepath.Join(root, owner, "x.wiki.git"))
	svc := newDiskRepoService(db, root)
	deleteRepo(t, svc, repoID, ownerID)
	testutil.Exec(t, db, `UPDATE repositories SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, repoID)

	purgeExpired(t, svc)

	assertDirHolds(t, filepath.Join(root, owner))
}

func TestPurgeExpired_LeavesALaterDeletionOfTheSameName(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner, first, second := sameNameOwners(t, db)
	repo, wiki := filepath.Join(root, owner, "x.git"), filepath.Join(root, owner, "x.wiki.git")
	svc := newDiskRepoService(db, root)
	firstID := seedRepoRow(t, db, first, owner, "x", "NULL")
	mkdirs(t, filepath.Join(repo, "first"), filepath.Join(wiki, "first"))
	deleteRepo(t, svc, firstID, first)
	secondID := seedRepoRow(t, db, second, owner, "x", "NULL")
	mkdirs(t, filepath.Join(repo, "second"), filepath.Join(wiki, "second"))
	deleteRepo(t, svc, secondID, second)
	testutil.Exec(t, db, `UPDATE repositories SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, firstID)

	purgeExpired(t, svc)
	restoreRepo(t, svc, secondID, second)

	assertExists(t, filepath.Join(repo, "second"), filepath.Join(wiki, "second"))
	assertDirHolds(t, filepath.Join(root, owner), "x.git", "x.wiki.git")
}

func TestRestore_RestoresTheRowsOwnCopy(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner, first, second := sameNameOwners(t, db)
	repo, wiki := filepath.Join(root, owner, "x.git"), filepath.Join(root, owner, "x.wiki.git")
	svc := newDiskRepoService(db, root)
	firstID := seedRepoRow(t, db, first, owner, "x", "NULL")
	mkdirs(t, filepath.Join(repo, "first"), filepath.Join(wiki, "first"))
	deleteRepo(t, svc, firstID, first)
	secondID := seedRepoRow(t, db, second, owner, "x", "NULL")
	mkdirs(t, filepath.Join(repo, "second"), filepath.Join(wiki, "second"))
	deleteRepo(t, svc, secondID, second)

	restoreRepo(t, svc, firstID, first)

	assertExists(t, filepath.Join(repo, "first"), filepath.Join(wiki, "first"))
	assertMissing(t, filepath.Join(repo, "second"), filepath.Join(wiki, "second"))
}

func deletedAtSQL(at time.Time) string {
	return fmt.Sprintf("to_timestamp(%d)", at.Unix())
}

// oldFormatCopy names a copy the way Delete did before copies were named after
// their row: by the Unix time of the deletion.
func oldFormatCopy(repoPath string, at time.Time) string {
	return repoPath + ".deleted." + strconv.FormatInt(at.Unix(), 10)
}

func TestRestore_OldFormatCopy_TakesTheRowsOwnCopy(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner, first, second := sameNameOwners(t, db)
	repo := filepath.Join(root, owner, "x.git")
	firstAt, secondAt := time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)
	firstID := seedRepoRow(t, db, first, owner, "x", deletedAtSQL(firstAt))
	seedRepoRow(t, db, second, owner, "x", deletedAtSQL(secondAt))
	mkdirs(t, filepath.Join(oldFormatCopy(repo, firstAt), "first"), filepath.Join(oldFormatCopy(repo, secondAt), "second"))

	restoreRepo(t, newDiskRepoService(db, root), firstID, first)

	assertExists(t, filepath.Join(repo, "first"), oldFormatCopy(repo, secondAt))
}

// A row whose old-format deletion left no copy must not claim a later
// deletion's copy.
func TestPurgeExpired_OldFormatCopy_NeverTakesALaterDeletion(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	owner, first, second := sameNameOwners(t, db)
	repo := filepath.Join(root, owner, "x.git")
	secondAt := time.Now().Add(-24 * time.Hour)
	seedRepoRow(t, db, first, owner, "x", deletedAtSQL(time.Now().Add(-31*24*time.Hour)))
	seedRepoRow(t, db, second, owner, "x", deletedAtSQL(secondAt))
	mkdirs(t, oldFormatCopy(repo, secondAt))

	purgeExpired(t, newDiskRepoService(db, root))

	assertExists(t, oldFormatCopy(repo, secondAt))
}
