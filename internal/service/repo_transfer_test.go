package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

func (e repoDirsEnv) offer(t *testing.T, repo *model.Repository, fromID int64, to string) *model.RepoTransfer {
	t.Helper()
	transfer, err := e.repos.TransferRepo(context.Background(), repo, fromID, to)
	if err != nil {
		t.Fatalf("TransferRepo to %s: %v", to, err)
	}
	if transfer == nil {
		t.Fatalf("the transfer to user %s moved the repo without their acceptance", to)
	}
	return transfer
}

func (e repoDirsEnv) personalRepo(t *testing.T, ownerID int64, owner, name string) *model.Repository {
	t.Helper()
	repo, err := e.repos.Create(context.Background(), ownerID, owner, name, "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create %s/%s: %v", owner, name, err)
	}
	return repo
}

func (e repoDirsEnv) wantUnder(t *testing.T, repoID int64, owner string) {
	t.Helper()
	got, err := e.repos.GetByID(context.Background(), repoID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.OwnerName != owner {
		t.Errorf("repo is under %s, want %s", got.OwnerName, owner)
	}
	if gitDir, _ := e.dirs(owner, got.Name); !pathExists(gitDir) {
		t.Errorf("repo dir is not under %s", owner)
	}
}

// The attack this flow exists for: a repo named after the victim, carrying a
// README and a collaborator the attacker controls, would become the victim's
// profile README the moment it landed in their namespace.
func TestRepoService_TransferToAUser_MovesNothingUntilAccepted(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	attackerID, attacker := env.seedUser(t)
	_, sidekick := env.seedUser(t)
	victimID, victim := env.seedUser(t)
	bait := env.personalRepo(t, attackerID, attacker, victim)
	if err := env.repos.AddCollaborator(ctx, bait, attackerID, sidekick, string(model.RoleAdmin)); err != nil {
		t.Fatalf("AddCollaborator: %v", err)
	}

	transfer := env.offer(t, bait, attackerID, victim)

	if transfer.RecipientID != victimID || transfer.RequesterName != attacker || transfer.FullName() != attacker+"/"+victim {
		t.Errorf("pending transfer = %+v", transfer)
	}
	if !transfer.ExpiresAt.After(transfer.CreatedAt.Add(service.RepoTransferTTL - time.Minute)) {
		t.Errorf("transfer expires at %v, want %v after %v", transfer.ExpiresAt, service.RepoTransferTTL, transfer.CreatedAt)
	}
	env.wantUnder(t, bait.ID, attacker)
	if n := env.rowCount(t, victim, victim); n != 0 {
		t.Fatalf("a %s/%s row exists before the victim accepted", victim, victim)
	}
	if gitDir, _ := env.dirs(victim, victim); pathExists(gitDir) {
		t.Fatal("the repo dir moved into the victim's namespace before they accepted")
	}

	incoming, err := env.repos.ListIncomingTransfers(ctx, victimID)
	if err != nil || len(incoming) != 1 || incoming[0].ID != transfer.ID {
		t.Fatalf("ListIncomingTransfers = %+v, %v; want the one transfer", incoming, err)
	}

	if _, err := env.repos.DeclineTransfer(ctx, transfer.ID, victimID); err != nil {
		t.Fatalf("DeclineTransfer: %v", err)
	}
	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, victimID, transfer.FullName()); !errors.Is(err, service.ErrTransferNotFound) {
		t.Errorf("accept after decline: want ErrTransferNotFound, got %v", err)
	}
	env.wantUnder(t, bait.ID, attacker)
}

func TestRepoService_AcceptTransfer_MovesTheRepoAndKeepsCollaborators(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	toID, to := env.seedUser(t)
	collabID, collab := env.seedUser(t)
	env.createWithWiki(t, from, "gift")
	repo, err := env.repos.Get(ctx, from, "gift")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := env.repos.AddCollaborator(ctx, repo, fromID, collab, string(model.RoleWriter)); err != nil {
		t.Fatalf("AddCollaborator: %v", err)
	}
	oldGit, oldWiki := env.dirs(from, "gift")
	head := headOf(t, oldGit)
	transfer := env.offer(t, repo, fromID, to)

	got, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, from+"/gift")
	if err != nil {
		t.Fatalf("AcceptTransfer: %v", err)
	}

	if got.OwnerID != toID || got.OrgID != 0 || got.OwnerName != to {
		t.Errorf("accepted repo: owner_id %d org_id %d owner_name %s; want %d, 0, %s", got.OwnerID, got.OrgID, got.OwnerName, toID, to)
	}
	env.wantLive(t, to, "gift", head, "wiki of "+from+"/gift")
	if pathExists(oldGit) || pathExists(oldWiki) {
		t.Error("dirs left at the old owner's path")
	}
	if env.repos.CanWrite(ctx, got, fromID) {
		t.Error("the previous owner can still write to the repo")
	}
	if !env.repos.CanWrite(ctx, got, collabID) {
		t.Error("the writer collaborator lost access")
	}
	if _, err := env.repos.PendingTransfer(ctx, repo.ID); !errors.Is(err, service.ErrTransferNotFound) {
		t.Errorf("transfer still pending after acceptance: %v", err)
	}
}

func TestRepoService_AcceptTransfer_OnlyTheRecipientOfWhatTheySaw(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	toID, to := env.seedUser(t)
	strangerID, _ := env.seedUser(t)
	repo := env.personalRepo(t, fromID, from, "only")
	transfer := env.offer(t, repo, fromID, to)

	for name, userID := range map[string]int64{"stranger": strangerID, "requester": fromID} {
		if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, userID, transfer.FullName()); !errors.Is(err, service.ErrTransferNotFound) {
			t.Errorf("%s accepting: want ErrTransferNotFound, got %v", name, err)
		}
		if _, err := env.repos.DeclineTransfer(ctx, transfer.ID, userID); !errors.Is(err, service.ErrTransferNotFound) {
			t.Errorf("%s declining: want ErrTransferNotFound, got %v", name, err)
		}
	}
	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, from+"/other"); !errors.Is(err, service.ErrTransferChanged) {
		t.Errorf("accepting a name the recipient was not shown: want ErrTransferChanged, got %v", err)
	}
	env.wantUnder(t, repo.ID, from)

	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, transfer.FullName()); err != nil {
		t.Fatalf("recipient accepting: %v", err)
	}
	env.wantUnder(t, repo.ID, to)
}

func TestRepoService_AcceptTransfer_RefusesAnExpiredTransfer(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	toID, to := env.seedUser(t)
	repo := env.personalRepo(t, fromID, from, "stale")
	transfer := env.offer(t, repo, fromID, to)
	if _, err := env.db.Exec(`UPDATE repo_transfers SET expires_at = NOW() - interval '1 second' WHERE id = $1`, transfer.ID); err != nil {
		t.Fatalf("expire: %v", err)
	}

	if incoming, err := env.repos.ListIncomingTransfers(ctx, toID); err != nil || len(incoming) != 0 {
		t.Errorf("ListIncomingTransfers = %+v, %v; want none", incoming, err)
	}
	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, transfer.FullName()); !errors.Is(err, service.ErrTransferNotFound) {
		t.Errorf("want ErrTransferNotFound, got %v", err)
	}
	env.wantUnder(t, repo.ID, from)
}

func TestRepoService_AcceptTransfer_KeepsTheTransferWhenTheNameIsTaken(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	toID, to := env.seedUser(t)
	repo := env.personalRepo(t, fromID, from, "clash")
	theirs := env.personalRepo(t, toID, to, "clash")
	transfer := env.offer(t, repo, fromID, to)

	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, transfer.FullName()); !errors.Is(err, service.ErrRepoNameTaken) {
		t.Fatalf("want ErrRepoNameTaken, got %v", err)
	}
	env.wantUnder(t, repo.ID, from)
	env.wantUnder(t, theirs.ID, to)

	// Freeing the name lets the recipient accept the same transfer.
	org := env.createOrg(t, toID)
	if _, err := env.repos.TransferRepo(ctx, theirs, toID, org.Name); err != nil {
		t.Fatalf("move their repo into their org: %v", err)
	}
	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, transfer.FullName()); err != nil {
		t.Fatalf("accept after freeing the name: %v", err)
	}
	env.wantUnder(t, repo.ID, to)
}

func TestRepoService_TransferRepo_ANewTransferReplacesThePendingOne(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	firstID, first := env.seedUser(t)
	secondID, second := env.seedUser(t)
	repo := env.personalRepo(t, fromID, from, "twice")
	old := env.offer(t, repo, fromID, first)
	transfer := env.offer(t, repo, fromID, second)

	if _, err := env.repos.AcceptTransfer(ctx, old.ID, firstID, old.FullName()); !errors.Is(err, service.ErrTransferNotFound) {
		t.Errorf("first recipient accepting a replaced transfer: want ErrTransferNotFound, got %v", err)
	}
	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, secondID, transfer.FullName()); err != nil {
		t.Fatalf("second recipient accepting: %v", err)
	}
	env.wantUnder(t, repo.ID, second)
}

func TestRepoService_CancelTransfer(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	toID, to := env.seedUser(t)
	repo := env.personalRepo(t, fromID, from, "withdrawn")
	transfer := env.offer(t, repo, fromID, to)

	if _, err := env.repos.CancelTransfer(ctx, repo, toID); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("recipient cancelling: want ErrForbidden, got %v", err)
	}
	if _, err := env.repos.CancelTransfer(ctx, repo, fromID); err != nil {
		t.Fatalf("CancelTransfer: %v", err)
	}
	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, transfer.FullName()); !errors.Is(err, service.ErrTransferNotFound) {
		t.Errorf("accept after cancel: want ErrTransferNotFound, got %v", err)
	}
	if _, err := env.repos.CancelTransfer(ctx, repo, fromID); !errors.Is(err, service.ErrTransferNotFound) {
		t.Errorf("cancelling twice: want ErrTransferNotFound, got %v", err)
	}
	env.wantUnder(t, repo.ID, from)
}

// An org owner's offer stands only while they own the org's repo.
func TestRepoService_Transfer_LapsesWhenTheRequesterStopsOwningTheRepo(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	o := env.orgRepoByCreator(t, "leaving")
	heirID, heir := env.seedUser(t)
	transfer := env.offer(t, o.repo, o.creatorID, heir)

	if err := env.orgs.UpdateMemberRole(ctx, o.org.ID, o.coOwnerID, o.creatorID, model.OrgRoleMember); err != nil {
		t.Fatalf("demote: %v", err)
	}

	if incoming, err := env.repos.ListIncomingTransfers(ctx, heirID); err != nil || len(incoming) != 0 {
		t.Errorf("ListIncomingTransfers = %+v, %v; want none", incoming, err)
	}
	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, heirID, transfer.FullName()); !errors.Is(err, service.ErrTransferNotFound) {
		t.Errorf("want ErrTransferNotFound, got %v", err)
	}
	env.wantUnder(t, o.repo.ID, o.org.Name)
}

func TestRepoService_Transfer_AMoveEndsThePendingTransfer(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	toID, to := env.seedUser(t)
	org := env.createOrg(t, fromID)
	repo := env.personalRepo(t, fromID, from, "rehomed")
	transfer := env.offer(t, repo, fromID, to)

	if moved, err := env.repos.TransferRepo(ctx, repo, fromID, org.Name); err != nil || moved != nil {
		t.Fatalf("TransferRepo into an owned org = %+v, %v; want it moved at once", moved, err)
	}

	if _, err := env.repos.AcceptTransfer(ctx, transfer.ID, toID, org.Name+"/rehomed"); !errors.Is(err, service.ErrTransferNotFound) {
		t.Errorf("accepting a transfer made before the repo moved: want ErrTransferNotFound, got %v", err)
	}
	env.wantUnder(t, repo.ID, org.Name)
}

// The recipient can't read the private repo they are offered, which would
// otherwise keep the notification out of their digest.
func TestNotification_RepoTransfer_InTheDigestWhileOffered(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	fromID, from := env.seedUser(t)
	toID, to := env.seedUser(t)
	users := service.NewUserService(store.NewUserStore(env.db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	notifs := service.NewNotificationService(store.NewNotificationStore(env.db), store.NewWatchStore(env.db), env.repos, service.NewEmailService(config.SMTPConfig{}), users)
	prefs := model.NotificationPrefs{EmailNotifications: true, EmailDigest: model.EmailDigestDaily, NotifyPRReview: true, NotifyMention: true}
	if err := users.UpdateNotificationPrefs(ctx, toID, prefs); err != nil {
		t.Fatalf("UpdateNotificationPrefs: %v", err)
	}
	transfer := env.offer(t, env.personalRepo(t, fromID, from, "offered"), fromID, to)
	notifs.NotifyRepoTransfer(ctx, *transfer)

	digest := func() []model.Notification {
		t.Helper()
		u, err := users.GetByID(ctx, toID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		got, err := notifs.ListUnreadForDigest(ctx, u, model.EmailDigestDaily)
		if err != nil {
			t.Fatalf("ListUnreadForDigest: %v", err)
		}
		return got
	}
	if got := digest(); len(got) != 1 || got[0].Type != model.NotifRepoTransfer || got[0].SubjectID != transfer.ID {
		t.Fatalf("digest while offered = %+v, want the transfer notification", got)
	}
	if _, err := env.repos.DeclineTransfer(ctx, transfer.ID, toID); err != nil {
		t.Fatalf("DeclineTransfer: %v", err)
	}
	if got := digest(); len(got) != 0 {
		t.Errorf("digest after declining = %+v, want nothing", got)
	}
}

// Moving an org repo into the requester's own account needs no one else's
// consent.
func TestRepoService_Transfer_ToYourselfIsImmediate(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	o := env.orgRepoByCreator(t, "mine")

	transfer, err := env.repos.TransferRepo(ctx, o.repo, o.creatorID, o.creator)
	if err != nil || transfer != nil {
		t.Fatalf("TransferRepo to yourself = %+v, %v; want it moved at once", transfer, err)
	}
	env.wantUnder(t, o.repo.ID, o.creator)
}
