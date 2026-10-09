package ssh

import (
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const quotaBytes = 10_000

func (r pushRepo) setSize(t *testing.T, bytes int64) {
	t.Helper()
	testutil.Exec(t, r.db, `UPDATE repositories SET size_bytes = $2 WHERE id = $1`, r.id, bytes)
}

func (r pushRepo) size(t *testing.T) (size int64, measured bool) {
	t.Helper()
	var n *int64
	if err := r.db.QueryRow(`SELECT size_bytes FROM repositories WHERE id = $1`, r.id).Scan(&n); err != nil {
		t.Fatalf("read size_bytes: %v", err)
	}
	if n == nil {
		return 0, false
	}
	return *n, true
}

func TestReceivePack_PushIsRefusedWhenTheStorageQuotaIsFull(t *testing.T) {
	r := seedPushRepoWithQuota(t, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: quotaBytes}})
	r.setSize(t, quotaBytes)

	got := r.receivePack(t, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	if got.status != 1 || !strings.Contains(got.stderr, "storage quota reached (9.8 KiB of 9.8 KiB)") {
		t.Errorf("want exit 1 and the quota message on stderr, got exit %d, stderr %q", got.status, got.stderr)
	}
	if tip, err := r.git.Reference(mainRef, true); err != nil || tip.Hash() != r.mainTip {
		t.Errorf("main moved to %v (%v) although the push was refused", tip, err)
	}
}

func TestReceivePack_PushThatFitsTheStorageQuotaIsAcceptedAndRemeasured(t *testing.T) {
	r := seedPushRepoWithQuota(t, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: 1 << 30}})
	r.setSize(t, 1)

	r.push(t, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if size, _ := r.size(t); size > 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("size_bytes still 1 five seconds after the push: nothing re-measured the repo")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReceivePack_DeleteOnlyPushGoesThroughAFullStorageQuota(t *testing.T) {
	r := seedPushRepoWithQuota(t, config.QuotaConfig{User: config.QuotaLimits{StorageBytes: quotaBytes}})
	r.setSize(t, 5*quotaBytes)

	got := r.receivePackOf(t, nil, &packp.Command{Name: featureRef, Old: r.featureTip, New: plumbing.ZeroHash})

	if got.status != 0 {
		t.Fatalf("delete-only push: exit %d, stderr %q", got.status, got.stderr)
	}
	if _, err := r.git.Reference(featureRef, true); err == nil {
		t.Error("feature still exists after the delete-only push")
	}
}

func TestReceivePack_NoStorageQuotaMeansNoCap(t *testing.T) {
	r := seedPushRepoWithQuota(t, config.QuotaConfig{User: config.QuotaLimits{Repos: 5}})
	r.setSize(t, 1<<40)

	r.push(t, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})
}
