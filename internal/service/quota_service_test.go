package service_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type quotaEnv struct {
	repoDirsEnv
	quota *service.QuotaService
}

// newQuotaEnv wires a QuotaService with cfg into the repo and org services, as service.New does.
func newQuotaEnv(t *testing.T, cfg config.QuotaConfig) quotaEnv {
	t.Helper()
	e := newRepoDirsEnv(t)
	q := service.NewQuotaService(store.NewRepoStore(e.db), store.NewUserStore(e.db), cfg, e.root)
	e.repos.WithQuota(q).WithTransferStore(store.NewRepoTransferStore(e.db))
	e.orgs.WithQuota(q)
	t.Cleanup(q.Wait)
	return quotaEnv{repoDirsEnv: e, quota: q}
}

func (e quotaEnv) seedOrg(t *testing.T, ownerID int64) (id int64, name string) {
	t.Helper()
	name = "org_" + testutil.UniqueSuffix(t)
	if err := e.db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, id)
	testutil.Exec(t, e.db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, id, ownerID)
	return id, name
}

func (e quotaEnv) setSize(t *testing.T, repoID, bytes int64) {
	t.Helper()
	testutil.Exec(t, e.db, `UPDATE repositories SET size_bytes = $2 WHERE id = $1`, repoID, bytes)
}

func wantQuotaErr(t *testing.T, err error, msg string) {
	t.Helper()
	if !errors.Is(err, service.ErrQuotaReached) {
		t.Fatalf("want a quota refusal, got %v", err)
	}
	if err.Error() != msg {
		t.Errorf("message: want %q, got %q", msg, err.Error())
	}
}

func TestQuotaService_NoLimitsMeansNoRefusals(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{})
	ctx := context.Background()
	id, name := e.seedUser(t)
	for i := range 3 {
		if _, err := e.repos.Create(ctx, id, name, "r"+string(rune('a'+i)), "", false, service.RepoInitOptions{}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	if err := e.quota.CheckNewRepo(ctx, service.QuotaOwner{UserID: id}); err != nil {
		t.Errorf("CheckNewRepo: %v", err)
	}
}

func TestQuotaService_CheckNewRepoRefusesAtTheCountAndStatesIt(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{User: config.QuotaLimits{Repos: 2}})
	ctx := context.Background()
	id, name := e.seedUser(t)
	owner := service.QuotaOwner{UserID: id}
	first, err := e.repos.Create(ctx, id, name, "one", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.repos.Create(ctx, id, name, "two", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatal(err)
	}

	wantQuotaErr(t, e.quota.CheckNewRepo(ctx, owner), "repository quota reached (2 of 2)")

	if err := e.repos.Delete(ctx, first.ID, id); err != nil {
		t.Fatal(err)
	}
	if err := e.quota.CheckNewRepo(ctx, owner); err != nil {
		t.Errorf("a soft-deleted repo must free its slot at once: %v", err)
	}
}

func TestQuotaService_ASuperadminsPersonalAccountIsExemptButOrgsAreNot(t *testing.T) {
	limits := config.QuotaLimits{Repos: 1}
	e := newQuotaEnv(t, config.QuotaConfig{User: limits, Org: limits})
	ctx := context.Background()
	adminID := testutil.SeedSuperadmin(t, e.db, testutil.UniqueSuffix(t))
	var adminName string
	if err := e.db.QueryRow(`SELECT username FROM users WHERE id = $1`, adminID).Scan(&adminName); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a", "b"} {
		if _, err := e.repos.Create(ctx, adminID, adminName, n, "", false, service.RepoInitOptions{}); err != nil {
			t.Fatalf("superadmin create %s: %v", n, err)
		}
	}

	orgID, _ := e.seedOrg(t, adminID)
	if _, err := e.orgs.CreateRepo(ctx, orgID, adminID, "first", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("first org repo: %v", err)
	}
	_, err := e.orgs.CreateRepo(ctx, orgID, adminID, "second", "", false, service.RepoInitOptions{})
	wantQuotaErr(t, err, "repository quota reached (1 of 1)")
}

func TestQuotaService_CheckStorageRefusesAtOrOverTheQuota(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: 1000}})
	ctx := context.Background()
	id, name := e.seedUser(t)
	repo, err := e.repos.Create(ctx, id, name, "big", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}

	e.setSize(t, repo.ID, 999)
	if err := e.quota.CheckStorage(ctx, repo); err != nil {
		t.Errorf("999 of 1000 bytes: %v", err)
	}
	e.setSize(t, repo.ID, 1000)
	wantQuotaErr(t, e.quota.CheckStorage(ctx, repo), "storage quota reached (1000 B of 1000 B)")
	e.setSize(t, repo.ID, 4000)
	wantQuotaErr(t, e.quota.CheckStorage(ctx, repo), "storage quota reached (3.9 KiB of 1000 B)")
}

func pushCmds(actions ...packp.Action) []*packp.Command {
	var cmds []*packp.Command
	for i, a := range actions {
		c := &packp.Command{Name: plumbing.ReferenceName("refs/heads/b" + string(rune('0'+i)))}
		switch a {
		case packp.Create:
			c.New = plumbing.NewHash("1111111111111111111111111111111111111111")
		case packp.Delete:
			c.Old = plumbing.NewHash("1111111111111111111111111111111111111111")
		default:
			c.Old = plumbing.NewHash("1111111111111111111111111111111111111111")
			c.New = plumbing.NewHash("2222222222222222222222222222222222222222")
		}
		cmds = append(cmds, c)
	}
	return cmds
}

func limiterAfterCommands(t *testing.T, body string, maxPack int64) *gittransport.LimitedReadCloser {
	t.Helper()
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader(body)), maxPack)
	if _, err := io.ReadFull(l, make([]byte, 10)); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestQuotaService_CapPushStopsAPackAtTheSpaceLeft(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: 1000}})
	ctx := context.Background()
	id, name := e.seedUser(t)
	repo, err := e.repos.Create(ctx, id, name, "r", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e.setSize(t, repo.ID, 900)
	l := limiterAfterCommands(t, strings.Repeat("x", 500), 1<<30)

	refusal, err := e.quota.CapPush(ctx, repo, pushCmds(packp.Create), l)
	if err != nil {
		t.Fatalf("CapPush: %v", err)
	}
	if refusal == nil {
		t.Fatal("want a quota refusal to send if the push crosses the cap")
	}
	wantQuotaErr(t, refusal, "storage quota reached (900 B of 1000 B)")
	if _, err := io.ReadAll(l); !errors.Is(err, gittransport.ErrPackTooLarge) {
		t.Errorf("a 490 byte pack with 100 bytes left: want ErrPackTooLarge, got %v", err)
	}
}

func TestQuotaService_CapPushAcceptsAPackThatFits(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: 1000}})
	ctx := context.Background()
	id, name := e.seedUser(t)
	repo, err := e.repos.Create(ctx, id, name, "r", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e.setSize(t, repo.ID, 100)
	l := limiterAfterCommands(t, strings.Repeat("x", 500), 1<<30)

	if _, err := e.quota.CapPush(ctx, repo, pushCmds(packp.Update), l); err != nil {
		t.Fatalf("CapPush: %v", err)
	}
	if _, err := io.ReadAll(l); err != nil {
		t.Errorf("a 490 byte pack with 900 bytes left: %v", err)
	}
}

func TestQuotaService_CapPushRefusesAnyPackWhenNothingIsLeftButNotADeleteOnlyPush(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: 1000}})
	ctx := context.Background()
	id, name := e.seedUser(t)
	repo, err := e.repos.Create(ctx, id, name, "r", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e.setSize(t, repo.ID, 5000)

	pack := limiterAfterCommands(t, strings.Repeat("x", 50), 1<<30)
	refusal, err := e.quota.CapPush(ctx, repo, pushCmds(packp.Create, packp.Delete), pack)
	if err != nil || refusal == nil {
		t.Fatalf("CapPush with a create: refusal %v, err %v", refusal, err)
	}
	if _, err := io.ReadAll(pack); !errors.Is(err, gittransport.ErrPackTooLarge) {
		t.Errorf("a push of new objects into a full quota: want ErrPackTooLarge, got %v", err)
	}

	del := limiterAfterCommands(t, "just ten b", 1<<30)
	if refusal, err := e.quota.CapPush(ctx, repo, pushCmds(packp.Delete, packp.Delete), del); err != nil || refusal != nil {
		t.Fatalf("CapPush with deletes only: refusal %v, err %v", refusal, err)
	}
	if _, err := io.ReadAll(del); err != nil {
		t.Errorf("a delete-only push must go through: %v", err)
	}
}

func TestQuotaService_CapPushLeavesTheGeneralPackCapAloneWhenItIsTighter(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: 1 << 30}})
	ctx := context.Background()
	id, name := e.seedUser(t)
	repo, err := e.repos.Create(ctx, id, name, "r", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l := limiterAfterCommands(t, strings.Repeat("x", 500), 100)

	refusal, err := e.quota.CapPush(ctx, repo, pushCmds(packp.Create), l)
	if err != nil || refusal != nil {
		t.Errorf("git.max_pack_bytes is the tighter cap, so the quota has no refusal to send: refusal %v, err %v", refusal, err)
	}
}

func TestQuotaService_CapPushIsNotAppliedWithoutAStorageQuota(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{User: config.QuotaLimits{Repos: 5}})
	ctx := context.Background()
	id, name := e.seedUser(t)
	repo, err := e.repos.Create(ctx, id, name, "r", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l := limiterAfterCommands(t, strings.Repeat("x", 500), 0)

	if refusal, err := e.quota.CapPush(ctx, repo, pushCmds(packp.Create), l); err != nil || refusal != nil {
		t.Fatalf("refusal %v, err %v", refusal, err)
	}
	if _, err := io.ReadAll(l); err != nil {
		t.Errorf("no storage quota: %v", err)
	}
}

func TestQuotaService_RecomputeStoresTheSizeOfTheGitAndWikiDirs(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{})
	ctx := context.Background()
	id, name := e.seedUser(t)
	repo, err := e.repos.Create(ctx, id, name, "r", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gitDir, wikiDir := e.dirs(name, "r")
	if err := os.MkdirAll(filepath.Join(wikiDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, n := range map[string]int{filepath.Join(gitDir, "blob-a"): 1000, filepath.Join(wikiDir, "objects", "blob-b"): 234} {
		if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := service.DirSize(gitDir, wikiDir)
	if err != nil {
		t.Fatal(err)
	}
	if want < 1234 {
		t.Fatalf("DirSize = %d, want at least the 1234 bytes written", want)
	}

	e.quota.Recompute(repo)
	e.quota.Wait()

	repos, bytes, err := store.NewRepoStore(e.db).OwnerUsage(ctx, id, 0)
	if err != nil || repos != 1 || bytes != want {
		t.Errorf("OwnerUsage = %d repos, %d bytes, %v; want 1 repo and %d bytes", repos, bytes, err, want)
	}
}

func TestQuotaService_RecomputeOfAMissingRepoStoresZero(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{})
	id, name := e.seedUser(t)
	repoID := testutil.SeedRepo(t, e.db, id, name, testutil.UniqueSuffix(t))
	e.setSize(t, repoID, 77)

	e.quota.Recompute(&model.Repository{ID: repoID, OwnerName: name, Name: "no-such-dir"})
	e.quota.Wait()

	if _, bytes, _ := store.NewRepoStore(e.db).OwnerUsage(context.Background(), id, 0); bytes != 0 {
		t.Errorf("a repo with no dirs: want 0 bytes, got %d", bytes)
	}
}

func TestQuotaService_BackfillMeasuresOnlyUnmeasuredRepos(t *testing.T) {
	e := newQuotaEnv(t, config.QuotaConfig{})
	ctx := context.Background()
	id, name := e.seedUser(t)
	fresh, err := e.repos.Create(ctx, id, name, "fresh", "", false, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatal(err)
	}
	e.quota.Wait()
	testutil.Exec(t, e.db, `UPDATE repositories SET size_bytes = NULL WHERE id = $1`, fresh.ID)
	measured, err := e.repos.Create(ctx, id, name, "measured", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e.quota.Wait()
	e.setSize(t, measured.ID, 12345)

	e.quota.Backfill(ctx)

	var gotFresh, gotMeasured int64
	if err := e.db.QueryRow(`SELECT size_bytes FROM repositories WHERE id = $1`, fresh.ID).Scan(&gotFresh); err != nil {
		t.Fatalf("the unmeasured repo must have a size after Backfill: %v", err)
	}
	if gotFresh <= 0 {
		t.Errorf("backfilled size = %d, want the repo's on-disk size", gotFresh)
	}
	if err := e.db.QueryRow(`SELECT size_bytes FROM repositories WHERE id = $1`, measured.ID).Scan(&gotMeasured); err != nil || gotMeasured != 12345 {
		t.Errorf("a measured repo keeps its size: got %d, %v", gotMeasured, err)
	}
}

func TestQuotaUsage_SummaryNamesOnlyTheLimitsThatAreSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    service.QuotaUsage
		want string
	}{
		{"none set", service.QuotaUsage{Repos: 3, Bytes: 99}, ""},
		{"repos only", service.QuotaUsage{Repos: 12, Limits: config.QuotaLimits{Repos: 50}}, "Repositories 12 of 50"},
		{"storage only", service.QuotaUsage{Bytes: 1288490189, Limits: config.QuotaLimits{StorageBytes: 10 << 30}}, "Storage 1.2 GiB of 10 GiB"},
		{"both", service.QuotaUsage{Repos: 12, Bytes: 1288490189, Limits: config.QuotaLimits{Repos: 50, StorageBytes: 10 << 30}},
			"Repositories 12 of 50 · Storage 1.2 GiB of 10 GiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.u.Summary(); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}
