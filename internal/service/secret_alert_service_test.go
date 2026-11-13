package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newSecretAlertService(t *testing.T) (*SecretAlertService, int64, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	owner := testutil.SeedUser(t, db, suffix)
	repo := testutil.SeedRepo(t, db, owner, "owner"+suffix, suffix)
	return NewSecretAlertService(store.New(db).SecretAlert), owner, repo
}

func finding(typ, value string) secretscan.Finding {
	return secretscan.Finding{Type: typ, Line: 7, Value: value, Confidence: secretscan.High}
}

func where(sha string) model.SecretSighting {
	return model.SecretSighting{CommitSHA: sha, FilePath: ".env", Ref: "refs/heads/main"}
}

func eventKinds(t *testing.T, svc *SecretAlertService, alertID int64) []string {
	t.Helper()
	events, err := svc.Events(context.Background(), alertID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}

func sameKinds(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestSecretAlertService_RecordHashesAndMasksTheValue(t *testing.T) {
	svc, _, repo := newSecretAlertService(t)
	ctx := context.Background()
	f := finding("github_token", "ghp_0123456789abcdefghijklmnopqrstuvwxyz")

	a, created, err := svc.Record(ctx, repo, f, where("c1"), model.SecretEventFound)
	if err != nil || !created {
		t.Fatalf("Record = %v, %v", created, err)
	}
	if a.SecretHash != secretscan.Hash(repo, f.Value) || a.SecretHint != secretscan.Mask(f.Value) {
		t.Errorf("alert hash/hint = %q/%q, want the scanner's", a.SecretHash, a.SecretHint)
	}
	locs, _ := svc.Locations(ctx, a.ID)
	if len(locs) != 1 || locs[0].LineNumber != 7 {
		t.Errorf("locations = %+v, want the finding's line", locs)
	}
	if _, _, err := svc.Record(ctx, repo, f, where("c1"), "closed"); err == nil {
		t.Error("Record accepted an event kind that is not found or blocked")
	}
}

func TestSecretAlertService_CloseReopenAllowTransitions(t *testing.T) {
	svc, owner, repo := newSecretAlertService(t)
	ctx := context.Background()
	a, _, _ := svc.Record(ctx, repo, finding("github_token", "ghp_0123456789abcdefghijklmnopqrstuvwxyz"), where("c1"), model.SecretEventFound)

	if err := svc.Close(ctx, a.ID, owner, "bogus"); !errors.Is(err, ErrSecretResolution) {
		t.Errorf("Close with a bogus resolution: %v", err)
	}
	if err := svc.Close(ctx, a.ID, owner, model.SecretResolutionRevoked); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(ctx, a.ID, owner, model.SecretResolutionRevoked); !errors.Is(err, ErrSecretAlertState) {
		t.Errorf("closing a closed alert: %v", err)
	}
	if closed, _ := svc.IsClosed(ctx, repo, a.SecretHash); !closed {
		t.Error("IsClosed = false after Close")
	}
	if err := svc.Reopen(ctx, a.ID, owner); err != nil {
		t.Fatal(err)
	}
	if closed, _ := svc.IsClosed(ctx, repo, a.SecretHash); closed {
		t.Error("IsClosed = true after Reopen")
	}
	if err := svc.Allow(ctx, a.ID, owner, model.SecretResolutionUsedInTests); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(ctx, repo, a.Number)
	if got.State != model.SecretAlertClosed || got.Resolution != model.SecretResolutionUsedInTests {
		t.Errorf("allowed alert = %+v", got)
	}
	if want := []string{"found", "closed", "reopened", "allowed"}; !sameKinds(eventKinds(t, svc, a.ID), want...) {
		t.Errorf("events = %v, want %v", eventKinds(t, svc, a.ID), want)
	}
}

func TestSecretAlertService_AllowRefusals(t *testing.T) {
	svc, owner, repo := newSecretAlertService(t)
	ctx := context.Background()
	token, _, _ := svc.Record(ctx, repo, finding(model.SecretTypeCloudzillaToken, "czp_0123456789abcdef0123456789abcdef"), where("c1"), model.SecretEventBlocked)
	other, _, _ := svc.Record(ctx, repo, finding("github_token", "ghp_0123456789abcdefghijklmnopqrstuvwxyz"), where("c1"), model.SecretEventFound)

	for _, res := range []string{model.SecretResolutionFalsePositive, model.SecretResolutionUsedInTests} {
		if err := svc.Allow(ctx, token.ID, owner, res); !errors.Is(err, ErrSecretAllowRefused) {
			t.Errorf("Allow of a Cloudzilla token as %s: %v, want ErrSecretAllowRefused", res, err)
		}
	}
	if err := svc.Allow(ctx, other.ID, owner, model.SecretResolutionRevoked); !errors.Is(err, ErrSecretAllowRefused) {
		t.Errorf("Allow as revoked: %v, want ErrSecretAllowRefused", err)
	}
	if got, _ := svc.Get(ctx, repo, token.Number); got.State != model.SecretAlertOpen {
		t.Error("a refused Allow closed the token alert")
	}
}

func TestSecretAlertService_AutoRevokeClosesWithoutAUser(t *testing.T) {
	svc, _, repo := newSecretAlertService(t)
	ctx := context.Background()
	a, _, _ := svc.Record(ctx, repo, finding(model.SecretTypeCloudzillaToken, "czp_0123456789abcdef0123456789abcdef"), where("c1"), model.SecretEventBlocked)

	if err := svc.AutoRevoke(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(ctx, repo, a.Number)
	if got.State != model.SecretAlertClosed || got.Resolution != model.SecretResolutionRevoked || !got.AutoRevoked || got.ResolvedBy != nil {
		t.Errorf("auto-revoked alert = %+v", got)
	}
	if want := []string{"blocked", "auto_revoked"}; !sameKinds(eventKinds(t, svc, a.ID), want...) {
		t.Errorf("events = %v, want %v", eventKinds(t, svc, a.ID), want)
	}
}

func TestSecretAlertService_OpenCountIgnoresBlockedOnlyAlerts(t *testing.T) {
	svc, _, repo := newSecretAlertService(t)
	ctx := context.Background()
	_, _, _ = svc.Record(ctx, repo, finding("github_token", "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), where("c1"), model.SecretEventBlocked)
	_, _, _ = svc.Record(ctx, repo, finding("github_token", "ghp_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), where("c2"), model.SecretEventFound)

	if n, err := svc.OpenCount(ctx, repo); err != nil || n != 1 {
		t.Fatalf("OpenCount = %d, %v; want 1", n, err)
	}
	if err := svc.MarkReached(ctx, repo, []string{"c1"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.OpenCount(ctx, repo); n != 2 {
		t.Errorf("OpenCount after MarkReached = %d, want 2", n)
	}
}

func TestSecretAlertService_SweepBlockedKeepsReachedAndRecentAlerts(t *testing.T) {
	svc, _, repo := newSecretAlertService(t)
	ctx := context.Background()
	blocked, _, _ := svc.Record(ctx, repo, finding("github_token", "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), where("c1"), model.SecretEventBlocked)
	_, _, _ = svc.Record(ctx, repo, finding("github_token", "ghp_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), where("c2"), model.SecretEventFound)

	if n, err := svc.SweepBlocked(ctx, time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("sweep before the cutoff = %d, %v; want 0", n, err)
	}
	// A cutoff in the future makes every alert old enough, so only the reached one may survive it.
	if n, err := svc.SweepBlocked(ctx, time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v; want 1", n, err)
	}
	if _, err := svc.Get(ctx, repo, blocked.Number); !errors.Is(err, ErrSecretAlertNotFound) {
		t.Errorf("blocked alert after the sweep: %v, want not found", err)
	}
	if n, _ := svc.OpenCount(ctx, repo); n != 1 {
		t.Errorf("OpenCount after the sweep = %d, want the reached alert", n)
	}
}
