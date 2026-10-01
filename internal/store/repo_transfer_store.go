package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ErrTransferNotFound covers a transfer that never existed, has ended or has
// expired: none of them can be acted on.
var ErrTransferNotFound = errors.New("repository transfer not found or expired")

// Expired rows are never swept: they read as gone, and the next transfer of
// the repo replaces them.
const pendingTransferSelect = `SELECT t.id, t.repo_id, r.owner_name, r.name, r.description, r.private,
	        t.requester_id, rq.username, t.recipient_id, rc.username, t.expires_at, t.created_at
	   FROM repo_transfers t
	   JOIN repositories r ON r.id = t.repo_id AND r.deleted_at IS NULL
	   JOIN users rq ON rq.id = t.requester_id
	   JOIN users rc ON rc.id = t.recipient_id
	  WHERE t.expires_at > NOW()`

// RepoTransferStore provides database operations for pending repository transfers.
type RepoTransferStore struct {
	db *sql.DB
}

// NewRepoTransferStore creates a RepoTransferStore backed by the given database.
func NewRepoTransferStore(db *sql.DB) *RepoTransferStore {
	return &RepoTransferStore{db: db}
}

// Create replaces any earlier transfer of the same repository. Like
// UpdateOwner, it refuses with ErrRepoChanged unless the repo is still live
// under ownerName, the owner it was read with.
func (s *RepoTransferStore) Create(ctx context.Context, t *model.RepoTransfer, ownerName string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo transfer create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM repo_transfers WHERE repo_id = $1`, t.RepoID); err != nil {
		return fmt.Errorf("repo transfer replace: %w", err)
	}
	err = tx.QueryRowContext(ctx,
		`INSERT INTO repo_transfers (repo_id, requester_id, recipient_id, expires_at)
		 SELECT id, $2, $3, $4 FROM repositories WHERE id = $1 AND owner_name = $5 AND deleted_at IS NULL
		 RETURNING id, created_at`,
		t.RepoID, t.RequesterID, t.RecipientID, t.ExpiresAt, ownerName,
	).Scan(&t.ID, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRepoChanged
	}
	if err != nil {
		return fmt.Errorf("repo transfer create: %w", err)
	}
	return tx.Commit()
}

func (s *RepoTransferStore) GetPending(ctx context.Context, id int64) (*model.RepoTransfer, error) {
	return s.getOne(ctx, pendingTransferSelect+` AND t.id = $1`, id)
}

func (s *RepoTransferStore) GetPendingByRepo(ctx context.Context, repoID int64) (*model.RepoTransfer, error) {
	return s.getOne(ctx, pendingTransferSelect+` AND t.repo_id = $1`, repoID)
}

func (s *RepoTransferStore) ListPendingForRecipient(ctx context.Context, userID int64) ([]model.RepoTransfer, error) {
	rows, err := s.db.QueryContext(ctx, pendingTransferSelect+` AND t.recipient_id = $1 ORDER BY t.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("repo transfer list for recipient: %w", err)
	}
	defer rows.Close()
	var out []model.RepoTransfer
	for rows.Next() {
		t, err := scanRepoTransfer(rows)
		if err != nil {
			return nil, fmt.Errorf("repo transfer list for recipient: %w", err)
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *RepoTransferStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM repo_transfers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("repo transfer delete: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("repo transfer delete rows affected: %w", err)
	} else if n == 0 {
		return ErrTransferNotFound
	}
	return nil
}

// Accept ends transfer id and moves its repo row to the recipient in one
// transaction, so a transfer cancelled, declined or expired meanwhile moves
// nothing. The row must still be live under oldOwnerName.
func (s *RepoTransferStore) Accept(ctx context.Context, id, recipientID int64, oldOwnerName, newOwnerName string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repo transfer accept: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var repoID int64
	err = tx.QueryRowContext(ctx,
		`DELETE FROM repo_transfers WHERE id = $1 AND recipient_id = $2 AND expires_at > NOW() RETURNING repo_id`,
		id, recipientID,
	).Scan(&repoID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTransferNotFound
	}
	if err != nil {
		return fmt.Errorf("repo transfer accept: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE repositories SET owner_id = $1, org_id = NULL, owner_name = $2, updated_at = NOW()
		 WHERE id = $3 AND owner_name = $4 AND deleted_at IS NULL`,
		recipientID, newOwnerName, repoID, oldOwnerName,
	)
	if err != nil {
		return repoWriteErr("accept repo transfer", err)
	}
	if err := requireRow(res, "accept repo transfer"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *RepoTransferStore) getOne(ctx context.Context, query string, arg int64) (*model.RepoTransfer, error) {
	t, err := scanRepoTransfer(s.db.QueryRowContext(ctx, query, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTransferNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repo transfer get: %w", err)
	}
	return t, nil
}

func scanRepoTransfer(row interface{ Scan(...any) error }) (*model.RepoTransfer, error) {
	t := &model.RepoTransfer{}
	err := row.Scan(&t.ID, &t.RepoID, &t.OwnerName, &t.RepoName, &t.Description, &t.Private,
		&t.RequesterID, &t.RequesterName, &t.RecipientID, &t.RecipientName, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}
