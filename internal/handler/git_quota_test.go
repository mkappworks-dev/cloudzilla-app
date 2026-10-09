package handler_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const httpQuotaBytes = 10_000

var userStorageQuota = config.QuotaConfig{User: config.QuotaLimits{StorageBytes: httpQuotaBytes}}

func TestGitReceivePack_PushIsRefusedWith413WhenTheStorageQuotaIsFull(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterWithQuota(t, db, reposRoot, userStorageQuota)
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `UPDATE repositories SET size_bytes = $2 WHERE id = $1`, r.id, httpQuotaBytes)

	rr := postReceivePack(t, h, r, true, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	if rr.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rr.Body.String(), "storage quota reached (9.8 KiB of 9.8 KiB)") {
		t.Errorf("want 413 and the quota message, got %d %q", rr.Code, rr.Body.String())
	}
	assertRef(t, r, "main", r.mainTip)
}

func TestGitReceivePack_PushThatFitsTheStorageQuotaIsAcceptedAndRemeasured(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterWithQuota(t, db, reposRoot, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: 1 << 30}})
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `UPDATE repositories SET size_bytes = 1 WHERE id = $1`, r.id)

	refs := receivePack(t, h, r, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	if refs[mainRef] != "ok" {
		t.Fatalf("main status = %q, want ok", refs[mainRef])
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var size int64
		if err := db.QueryRow(`SELECT size_bytes FROM repositories WHERE id = $1`, r.id).Scan(&size); err != nil {
			t.Fatal(err)
		}
		if size > 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("size_bytes still 1 five seconds after the push: nothing re-measured the repo")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestGitReceivePack_DeleteOnlyPushGoesThroughAFullStorageQuota(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterWithQuota(t, db, reposRoot, userStorageQuota)
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `UPDATE repositories SET size_bytes = $2 WHERE id = $1`, r.id, 5*httpQuotaBytes)

	rr := postPack(t, h, r, true, nil, &packp.Command{Name: featureRef, Old: r.featureTip, New: plumbing.ZeroHash})

	if refs := reportedRefs(t, rr); refs[featureRef] != "ok" {
		t.Errorf("feature status = %q, want ok", refs[featureRef])
	}
	if _, err := r.git.Reference(featureRef, true); err == nil {
		t.Error("feature still exists after the delete-only push")
	}
}

func TestGitReceivePack_WithoutAStorageQuotaTheSizeIsNotChecked(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterWithQuota(t, db, reposRoot, config.QuotaConfig{User: config.QuotaLimits{Repos: 5}})
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `UPDATE repositories SET size_bytes = $2 WHERE id = $1`, r.id, int64(1)<<40)

	refs := receivePack(t, h, r, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	if refs[mainRef] != "ok" {
		t.Errorf("main status = %q, want ok", refs[mainRef])
	}
}
