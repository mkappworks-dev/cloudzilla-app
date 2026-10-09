package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// RepoTransferTTL is how long a transfer to another user waits for an answer:
// a week, so it outlasts a weekly email digest.
const RepoTransferTTL = 7 * 24 * time.Hour

var ErrTransferNotFound = store.ErrTransferNotFound

// ErrTransferChanged: the repo was renamed or moved after the recipient was
// shown it, so accepting would hand them a name they never agreed to.
var ErrTransferChanged = errors.New("the repository was renamed or moved since you saw this transfer; review it again")

func (s *RepoService) WithTransferStore(transfers *store.RepoTransferStore) *RepoService {
	s.transfers = transfers
	return s
}

// TransferRepo hands repo to the user or org named newOwnerName, which resolves
// user first, as /{owner} does. The requester must own the repo, and must own
// a receiving org too. A repo for another user only moves once they accept, so
// TransferRepo returns that pending transfer instead of moving anything.
func (s *RepoService) TransferRepo(ctx context.Context, repo *model.Repository, requestingUserID int64, newOwnerName string) (*model.RepoTransfer, error) {
	if !s.IsOwner(ctx, repo, requestingUserID) {
		return nil, fmt.Errorf("only the repo owner can transfer ownership")
	}
	newOwnerID, newOrgID, err := s.transferTarget(ctx, requestingUserID, newOwnerName)
	if err != nil {
		return nil, err
	}
	if newOwnerID == repo.OwnerID && newOrgID == repo.OrgID {
		return nil, fmt.Errorf("the repository already belongs to %s", newOwnerName)
	}
	if err := s.quota.CheckNewRepo(ctx, QuotaOwner{UserID: newOwnerID, OrgID: newOrgID}); err != nil {
		return nil, err
	}
	if newOwnerID != 0 && newOwnerID != requestingUserID {
		return s.offerTransfer(ctx, repo, requestingUserID, newOwnerID)
	}
	return nil, s.moveRepo(ctx, repo, newOwnerName, func() error {
		return s.repos.UpdateOwner(ctx, repo.ID, repo.OwnerName, newOwnerID, newOrgID, newOwnerName)
	})
}

// transferTarget resolves name, user first, to a user or to an org the
// requester owns.
func (s *RepoService) transferTarget(ctx context.Context, requesterID int64, name string) (userID, orgID int64, err error) {
	user, err := s.users.GetByUsername(ctx, name)
	if err == nil {
		return user.ID, 0, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, err
	}
	org, err := s.orgs.GetByName(ctx, name)
	if err != nil {
		return 0, 0, fmt.Errorf("new owner not found: %w", err)
	}
	if !s.isOrgOwner(ctx, org.ID, requesterID) {
		return 0, 0, fmt.Errorf("only an owner of %s can transfer a repository into it", org.Name)
	}
	return 0, org.ID, nil
}

// The recipient's namespace is checked only on accept: refusing here would
// tell the requester whether the recipient holds a private repo of that name.
func (s *RepoService) offerTransfer(ctx context.Context, repo *model.Repository, requesterID, recipientID int64) (*model.RepoTransfer, error) {
	t := &model.RepoTransfer{
		RepoID:      repo.ID,
		RequesterID: requesterID,
		RecipientID: recipientID,
		ExpiresAt:   time.Now().Add(RepoTransferTTL),
	}
	if err := s.transfers.Create(ctx, t, repo.OwnerName); err != nil {
		return nil, err
	}
	return s.transfers.GetPending(ctx, t.ID)
}

// AcceptTransfer moves the repo of transfer id to userID, its recipient.
// offered is the owner/name the recipient was shown.
func (s *RepoService) AcceptTransfer(ctx context.Context, id, userID int64, offered string) (*model.Repository, error) {
	t, repo, err := s.incomingTransfer(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if t.FullName() != offered {
		return nil, ErrTransferChanged
	}
	recipient, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	// The offer was checked against the quota of its day; the recipient may have filled it since.
	if err := s.quota.CheckNewRepo(ctx, QuotaOwner{UserID: userID}); err != nil {
		return nil, err
	}
	err = s.moveRepo(ctx, repo, recipient.Username, func() error {
		return s.transfers.Accept(ctx, t.ID, userID, repo.OwnerName, recipient.Username)
	})
	if err != nil {
		return nil, err
	}
	return s.repos.GetByID(ctx, repo.ID)
}

// DeclineTransfer ends transfer id, offered to userID, without moving anything.
func (s *RepoService) DeclineTransfer(ctx context.Context, id, userID int64) (*model.RepoTransfer, error) {
	t, err := s.transfers.GetPending(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.RecipientID != userID {
		return nil, ErrTransferNotFound
	}
	return t, s.transfers.Delete(ctx, t.ID)
}

// CancelTransfer withdraws repo's pending transfer; any owner of the repo may.
func (s *RepoService) CancelTransfer(ctx context.Context, repo *model.Repository, userID int64) (*model.RepoTransfer, error) {
	if !s.IsOwner(ctx, repo, userID) {
		return nil, fmt.Errorf("only the repo owner can cancel a transfer: %w", ErrForbidden)
	}
	t, err := s.transfers.GetPendingByRepo(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	return t, s.transfers.Delete(ctx, t.ID)
}

func (s *RepoService) PendingTransfer(ctx context.Context, repoID int64) (*model.RepoTransfer, error) {
	return s.transfers.GetPendingByRepo(ctx, repoID)
}

// ListIncomingTransfers returns the transfers userID can accept.
func (s *RepoService) ListIncomingTransfers(ctx context.Context, userID int64) ([]model.RepoTransfer, error) {
	all, err := s.transfers.ListPendingForRecipient(ctx, userID)
	if err != nil {
		return nil, err
	}
	live := make([]model.RepoTransfer, 0, len(all))
	for _, t := range all {
		if _, _, err := s.incomingTransfer(ctx, t.ID, userID); err == nil {
			live = append(live, t)
		}
	}
	return live, nil
}

// incomingTransfer returns transfer id and its repo when userID may accept it.
// A transfer whose requester no longer owns the repo has lapsed.
func (s *RepoService) incomingTransfer(ctx context.Context, id, userID int64) (*model.RepoTransfer, *model.Repository, error) {
	t, err := s.transfers.GetPending(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if t.RecipientID != userID {
		return nil, nil, ErrTransferNotFound
	}
	repo, err := s.repos.GetByID(ctx, t.RepoID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !s.IsOwner(ctx, repo, t.RequesterID) {
		return nil, nil, ErrTransferNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return t, repo, nil
}

// moveRepo moves repo's git and wiki dirs into newOwnerName's namespace, then
// runs update to point the row there, moving the dirs back if it fails.
func (s *RepoService) moveRepo(ctx context.Context, repo *model.Repository, newOwnerName string, update func() error) error {
	if err := ValidateName(newOwnerName); err != nil {
		return fmt.Errorf("%w: owner %q", ErrInvalidRepoPath, newOwnerName)
	}

	oldGitDir, _ := repoDirs(s.cfg.ReposRoot, repo.OwnerName, repo.Name)
	oldWikiDir, err := s.ownWikiDir(ctx, repo.OwnerName, repo.Name)
	if err != nil {
		return err
	}
	newGitDir, newWikiDir := repoDirs(s.cfg.ReposRoot, newOwnerName, repo.Name)
	// os.Rename refuses an existing dir, but a repo without a wiki skips the
	// wiki move and would pick up whatever wiki waits at the new path.
	if pathTaken(newGitDir) || pathTaken(newWikiDir) {
		return ErrRepoNameTaken
	}
	if held, err := s.repos.NameHeld(ctx, newOwnerName, wikiPartner(repo.Name)); err != nil {
		return err
	} else if held {
		return ErrRepoNameTaken
	}

	if err := os.MkdirAll(filepath.Dir(newGitDir), 0755); err != nil {
		return fmt.Errorf("create owner dir: %w", err)
	}
	moves := []dirMove{{from: oldGitDir, to: newGitDir}}
	if oldWikiDir != "" {
		moves = append(moves, dirMove{from: oldWikiDir, to: newWikiDir})
	}
	moved, err := renameDirs(moves)
	if err != nil {
		return fmt.Errorf("move git dir: %w", err)
	}
	// renameDirs skips a missing source, and a missing git dir means repo is a
	// stale read: another request has moved or deleted it since.
	if len(moved) == 0 || moved[0].from != oldGitDir {
		revertDirs(moved)
		return ErrRepoChanged
	}

	if err := update(); err != nil {
		revertDirs(moved)
		return repoNameErr("update repo owner", err)
	}
	return nil
}
