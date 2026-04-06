package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/mkappworks/cloudzilla/internal/testutil"
)

// --- Pure-logic tests (no DB required) ---

func TestCanRead_PublicRepo_AnonymousAllowed(t *testing.T) {
	svc := newPermSvc(nil)
	repo := &model.Repository{Private: false, OwnerID: 1}
	if !svc.CanRead(context.Background(), repo, nil) {
		t.Error("public repo must be readable by anonymous users")
	}
}

func TestCanRead_PublicRepo_AnyUserAllowed(t *testing.T) {
	svc := newPermSvc(nil)
	repo := &model.Repository{Private: false, OwnerID: 1}
	uid := int64(999)
	if !svc.CanRead(context.Background(), repo, &uid) {
		t.Error("public repo must be readable by any authenticated user")
	}
}

func TestCanRead_PrivateRepo_AnonymousDenied(t *testing.T) {
	svc := newPermSvc(nil)
	repo := &model.Repository{Private: true, OwnerID: 1}
	if svc.CanRead(context.Background(), repo, nil) {
		t.Error("private repo must deny anonymous access")
	}
}

func TestCanRead_PrivateRepo_OwnerAllowed(t *testing.T) {
	svc := newPermSvc(nil)
	repo := &model.Repository{Private: true, OwnerID: 42}
	uid := int64(42)
	if !svc.CanRead(context.Background(), repo, &uid) {
		t.Error("owner must have read access to private repo")
	}
}

func TestCanWrite_Owner_Allowed(t *testing.T) {
	svc := newPermSvc(nil)
	repo := &model.Repository{Private: true, OwnerID: 42}
	if !svc.CanWrite(context.Background(), repo, 42) {
		t.Error("owner must have write access")
	}
}

func TestCanWrite_NonOwner_NoPermission_Denied(t *testing.T) {
	svc := newPermSvc(nil)
	repo := &model.Repository{Private: false, OwnerID: 1}
	// Non-owner with no permissions — broken DB causes GetPermission to fail → false.
	if svc.CanWrite(context.Background(), repo, 2) {
		t.Error("non-owner without permission must not have write access")
	}
}

func TestCanManage_Owner_Allowed(t *testing.T) {
	svc := newPermSvc(nil)
	repo := &model.Repository{OwnerID: 5}
	if !svc.CanManage(context.Background(), repo, 5) {
		t.Error("owner must have manage access")
	}
}

func TestIsOwner_Owner_True(t *testing.T) {
	svc := newPermSvc(nil)
	repo := &model.Repository{OwnerID: 7}
	if !svc.IsOwner(context.Background(), repo, 7) {
		t.Error("repo.OwnerID match must return IsOwner=true")
	}
}

func TestIsOwner_NonOwner_False(t *testing.T) {
	svc := newPermSvc(nil)
	repo := &model.Repository{OwnerID: 7}
	if svc.IsOwner(context.Background(), repo, 8) {
		t.Error("non-owner must not pass IsOwner")
	}
}

// --- Integration tests (require TEST_DATABASE_DSN) ---

func TestCanWrite_WriterRole_Allowed(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := fmt.Sprintf("%d_%d", os.Getpid(), 1)
	ownerID := testutil.SeedUser(t, db, "owner_"+suffix)
	userID := testutil.SeedUser(t, db, "writer_"+suffix)
	ownerName := "testuser_owner_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'writer')`,
		userID, repoID,
	)
	if err != nil {
		t.Fatalf("add writer permission: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(),
			`DELETE FROM permissions WHERE user_id = $1 AND repo_id = $2`, userID, repoID)
	})

	svc := newPermSvcDB(db)
	repo := &model.Repository{ID: repoID, OwnerID: ownerID, Private: true}
	if !svc.CanWrite(context.Background(), repo, userID) {
		t.Error("writer role must grant CanWrite")
	}
	if svc.CanManage(context.Background(), repo, userID) {
		t.Error("writer role must not grant CanManage")
	}
	if svc.IsOwner(context.Background(), repo, userID) {
		t.Error("writer role must not grant IsOwner")
	}
}

func TestCanWrite_ReaderRole_Denied(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := fmt.Sprintf("%d_%d", os.Getpid(), 2)
	ownerID := testutil.SeedUser(t, db, "owner2_"+suffix)
	userID := testutil.SeedUser(t, db, "reader2_"+suffix)
	ownerName := "testuser_owner2_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'reader')`,
		userID, repoID,
	)
	if err != nil {
		t.Fatalf("add reader permission: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(),
			`DELETE FROM permissions WHERE user_id = $1 AND repo_id = $2`, userID, repoID)
	})

	svc := newPermSvcDB(db)
	repo := &model.Repository{ID: repoID, OwnerID: ownerID, Private: true}
	uid := userID
	if !svc.CanRead(context.Background(), repo, &uid) {
		t.Error("reader role must grant CanRead on private repo")
	}
	if svc.CanWrite(context.Background(), repo, userID) {
		t.Error("reader role must not grant CanWrite")
	}
}

func TestCanManage_AdminRole_Allowed(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := fmt.Sprintf("%d_%d", os.Getpid(), 3)
	ownerID := testutil.SeedUser(t, db, "owner3_"+suffix)
	userID := testutil.SeedUser(t, db, "admin3_"+suffix)
	ownerName := "testuser_owner3_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'admin')`,
		userID, repoID,
	)
	if err != nil {
		t.Fatalf("add admin permission: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(),
			`DELETE FROM permissions WHERE user_id = $1 AND repo_id = $2`, userID, repoID)
	})

	svc := newPermSvcDB(db)
	repo := &model.Repository{ID: repoID, OwnerID: ownerID, Private: true}
	if !svc.CanWrite(context.Background(), repo, userID) {
		t.Error("admin role must grant CanWrite")
	}
	if !svc.CanManage(context.Background(), repo, userID) {
		t.Error("admin role must grant CanManage")
	}
	if svc.IsOwner(context.Background(), repo, userID) {
		t.Error("admin role must not grant IsOwner")
	}
}

func TestCanRead_PrivateRepo_NoPermission_Denied(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := fmt.Sprintf("%d_%d", os.Getpid(), 4)
	ownerID := testutil.SeedUser(t, db, "owner4_"+suffix)
	userID := testutil.SeedUser(t, db, "stranger_"+suffix)
	ownerName := "testuser_owner4_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	svc := newPermSvcDB(db)
	repo := &model.Repository{ID: repoID, OwnerID: ownerID, Private: true}
	uid := userID
	if svc.CanRead(context.Background(), repo, &uid) {
		t.Error("user with no permission must not read private repo")
	}
}

// --- helpers ---

// newPermSvc builds a RepoService backed by an unreachable DB (forces permission lookups to fail).
// Suitable for pure-logic tests that only exercise owner-ID comparisons.
func newPermSvc(_ interface{}) *service.RepoService {
	db, _ := sql.Open("pgx", "postgres://localhost:1/nonexistent?connect_timeout=1")
	return service.NewRepoService(
		store.NewRepoStore(db),
		store.NewUserStore(db),
		store.NewOrgStore(db),
		config.GitConfig{},
	)
}

// newPermSvcDB builds a RepoService backed by a real test DB.
func newPermSvcDB(db *sql.DB) *service.RepoService {
	return service.NewRepoService(
		store.NewRepoStore(db),
		store.NewUserStore(db),
		store.NewOrgStore(db),
		config.GitConfig{},
	)
}
