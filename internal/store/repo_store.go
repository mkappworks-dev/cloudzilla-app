package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	storedb "github.com/mkappworks/cloudzilla/internal/store/db"
)

type RepoStore struct {
	q  *storedb.Queries
	db *sql.DB
}

func NewRepoStore(q *storedb.Queries, database *sql.DB) *RepoStore {
	return &RepoStore{q: q, db: database}
}

func (s *RepoStore) Create(ctx context.Context, r *model.Repository) error {
	result, err := s.q.CreateRepo(ctx, storedb.CreateRepoParams{
		OwnerID:       r.OwnerID,
		Name:          r.Name,
		Description:   r.Description,
		Private:       r.Private,
		DefaultBranch: r.DefaultBranch,
	})
	if err != nil {
		return fmt.Errorf("repo create: %w", err)
	}
	r.ID = result.ID
	r.CreatedAt = result.CreatedAt
	r.UpdatedAt = result.UpdatedAt
	return nil
}

func (s *RepoStore) CreateWithOwnerName(ctx context.Context, r *model.Repository) error {
	now := time.Now().UTC()
	var orgID interface{}
	if r.OrgID != 0 {
		orgID = r.OrgID
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.OwnerID, r.OwnerName, orgID, r.Name, r.Description, r.Private, r.DefaultBranch, now, now,
	)
	if err != nil {
		return fmt.Errorf("repo create with owner name: %w", err)
	}
	r.ID, _ = res.LastInsertId()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (s *RepoStore) GetByOwnerName(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	r := &model.Repository{}
	var orgID sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at
		 FROM repositories WHERE owner_name = ? AND name = ?`,
		ownerName, name,
	).Scan(&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("repo get by owner name: %w", err)
	}
	if orgID.Valid {
		r.OrgID = orgID.Int64
	}
	return r, nil
}

func (s *RepoStore) GetByOwnerNameList(ctx context.Context, ownerName string) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at
		 FROM repositories WHERE owner_name = ? ORDER BY created_at DESC`,
		ownerName,
	)
	if err != nil {
		return nil, fmt.Errorf("repo list by owner name: %w", err)
	}
	defer rows.Close()
	var repos []model.Repository
	for rows.Next() {
		var r model.Repository
		var orgID sql.NullInt64
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.OwnerName, &orgID, &r.Name, &r.Description, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if orgID.Valid {
			r.OrgID = orgID.Int64
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (s *RepoStore) List(ctx context.Context) ([]model.Repository, error) {
	repos, err := s.q.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo list: %w", err)
	}
	return mapDBReposToModel(repos), nil
}

func (s *RepoStore) GetByOwnerAndName(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	result, err := s.q.GetRepoByOwnerAndName(ctx, storedb.GetRepoByOwnerAndNameParams{
		Username: ownerName,
		Name:     name,
	})
	if err != nil {
		return nil, fmt.Errorf("repo get: %w", err)
	}
	return mapDBRepoToModel(&result), nil
}

func (s *RepoStore) GetByOwnerID(ctx context.Context, ownerID int64) ([]model.Repository, error) {
	repos, err := s.q.GetReposByOwnerID(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("repo list by owner: %w", err)
	}
	return mapDBReposToModel(repos), nil
}

func (s *RepoStore) GetByOrgID(ctx context.Context, orgID int64) ([]model.Repository, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, owner_name, org_id, name, description, private, default_branch, created_at, updated_at
		 FROM repositories WHERE org_id = ? ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("repo list by org: %w", err)
	}
	defer rows.Close()
	var repos []model.Repository
	for rows.Next() {
		var r model.Repository
		var oid sql.NullInt64
		if err := rows.Scan(&r.ID, &r.OwnerID, &r.OwnerName, &oid, &r.Name, &r.Description, &r.Private, &r.DefaultBranch, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if oid.Valid {
			r.OrgID = oid.Int64
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (s *RepoStore) GetPermission(ctx context.Context, repoID, userID int64) (string, error) {
	role, err := s.q.GetPermission(ctx, storedb.GetPermissionParams{
		RepoID: repoID,
		UserID: userID,
	})
	if err != nil {
		return "", fmt.Errorf("get permission: %w", err)
	}
	return role, nil
}

func mapDBRepoToModel(dbRepo *storedb.Repository) *model.Repository {
	return &model.Repository{
		ID:            dbRepo.ID,
		OwnerID:       dbRepo.OwnerID,
		Name:          dbRepo.Name,
		Description:   dbRepo.Description,
		Private:       dbRepo.Private,
		DefaultBranch: dbRepo.DefaultBranch,
		CreatedAt:     dbRepo.CreatedAt,
		UpdatedAt:     dbRepo.UpdatedAt,
	}
}

func mapDBReposToModel(dbRepos []storedb.Repository) []model.Repository {
	repos := make([]model.Repository, len(dbRepos))
	for i, dbRepo := range dbRepos {
		repos[i] = *mapDBRepoToModel(&dbRepo)
	}
	return repos
}
