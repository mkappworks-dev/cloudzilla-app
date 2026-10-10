package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

// DeleteByID removes a user. Related rows depend on ON DELETE CASCADE in the schema.
func (s *UserStore) DeleteByID(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, userID)
	if err != nil {
		return fmt.Errorf("user delete: %w", err)
	}
	return nil
}

var ErrOwnedReposChanged = errors.New("user's repositories changed during deletion")

// soleOwnedOrgs selects the orgs user $1 is the only owner of.
const soleOwnedOrgs = `SELECT 1 FROM org_members om
	WHERE om.user_id = $1 AND om.role = 'owner'
	  AND NOT EXISTS (SELECT 1 FROM org_members other
	                  WHERE other.org_id = om.org_id AND other.role = 'owner' AND other.user_id <> $1)`

// IsSoleOrgOwner reports whether deleting userID would leave an org with no owner.
func (s *UserStore) IsSoleOrgOwner(ctx context.Context, userID int64) (bool, error) {
	var sole bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (`+soleOwnedOrgs+`)`, userID).Scan(&sole); err != nil {
		return false, fmt.Errorf("user sole org owner: %w", err)
	}
	return sole, nil
}

// ghostReassignments are the columns that reference users(id) with no ON DELETE
// action; any row left in one would block the user delete. nameCol, when set,
// is the username copied into the row, which would otherwise credit whoever
// registers the freed name next.
var ghostReassignments = []struct{ table, idCol, nameCol string }{
	{"issues", "author_id", ""},
	{"pull_requests", "author_id", ""},
	{"comments", "author_id", "author_name"},
	{"notifications", "actor_id", "actor_name"},
	{"invitations", "invited_by_id", ""},
	{"releases", "author_id", ""},
	{"commit_statuses", "creator_id", ""},
	{"pull_reviews", "author_id", "author_name"},
	{"pull_line_comments", "author_id", "author_name"},
	{"discussions", "author_id", "author_name"},
	{"discussion_replies", "author_id", "author_name"},
	{"pull_events", "actor_id", "actor_name"},
	{"issue_events", "actor_id", "actor_name"},
	{"repositories", "deleted_by", ""},
}

// The user's own repos go first, taking everything in them along, so only what
// the user wrote elsewhere passes to the ghost, as on GitHub.
// livePersonalIDs are the repos the caller already moved aside; locking the user
// row blocks new repo inserts (their FK check needs it), so the set is re-checked
// here and a repo created or restored in the meantime aborts the delete. Org
// repos have no owner_id, so neither the cascade nor this check touches them.
// It returns the deleted user's avatar key, read under the row lock.
func (s *UserStore) DeleteWithOwnedRepos(ctx context.Context, userID int64, livePersonalIDs []int64) (avatarKey string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("user delete begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Before the user's own row lock, so every path takes the superadmin locks first.
	if err := keepActiveSuperadmin(ctx, tx, userID); err != nil {
		return "", err
	}
	if err := tx.QueryRowContext(ctx, `SELECT avatar_key FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&avatarKey); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("user delete lock: %w", err)
	}
	// OrgStore.changeMember takes these locks too, so neither another owner
	// leaving nor this user's promotion can land between this check and the
	// delete. Orgs where the user is only a member count: a promotion would
	// otherwise let their last other owner leave.
	if _, err := tx.ExecContext(ctx,
		`SELECT 1 FROM organizations o JOIN org_members om ON om.org_id = o.id
		 WHERE om.user_id = $1 ORDER BY o.id FOR NO KEY UPDATE OF o`, userID); err != nil {
		return "", fmt.Errorf("user delete lock orgs: %w", err)
	}
	var sole bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (`+soleOwnedOrgs+`)`, userID).Scan(&sole); err != nil {
		return "", fmt.Errorf("user delete check orgs: %w", err)
	}
	if sole {
		return "", ErrLastOrgOwner
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM repositories WHERE owner_id=$1 AND deleted_at IS NULL FOR UPDATE`, userID)
	if err != nil {
		return "", fmt.Errorf("user delete list repos: %w", err)
	}
	var live []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", fmt.Errorf("user delete scan repo: %w", err)
		}
		live = append(live, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("user delete list repos: %w", err)
	}
	slices.Sort(live)
	expected := slices.Sorted(slices.Values(livePersonalIDs))
	if !slices.Equal(live, expected) {
		return "", ErrOwnedReposChanged
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE owner_id=$1`, userID); err != nil {
		return "", fmt.Errorf("user delete repos: %w", err)
	}
	// Review requests are addressed to the user, and the ghost can never answer one.
	if _, err := tx.ExecContext(ctx, `DELETE FROM pull_reviews WHERE author_id=$1 AND state='pending'`, userID); err != nil {
		return "", fmt.Errorf("user delete review requests: %w", err)
	}
	var ghostID int64
	var ghostName string
	if err := tx.QueryRowContext(ctx, `SELECT id, username FROM users WHERE id = ghost_user_id()`).Scan(&ghostID, &ghostName); err != nil {
		return "", fmt.Errorf("user delete load ghost: %w", err)
	}
	for _, r := range ghostReassignments {
		q := `UPDATE ` + r.table + ` SET ` + r.idCol + ` = $2`
		args := []any{userID, ghostID}
		if r.nameCol != "" {
			q += `, ` + r.nameCol + ` = $3`
			args = append(args, ghostName)
		}
		if _, err := tx.ExecContext(ctx, q+` WHERE `+r.idCol+` = $1`, args...); err != nil {
			return "", fmt.Errorf("user delete reassign %s.%s: %w", r.table, r.idCol, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, userID); err != nil {
		return "", fmt.Errorf("user delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("user delete commit: %w", err)
	}
	return avatarKey, nil
}
