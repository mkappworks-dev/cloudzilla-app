package service_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// newUnreachableOrgSvc backs OrgService with a database that fails every lookup,
// so only the admin-mode override can make a check pass.
func newUnreachableOrgSvc() *service.OrgService {
	db, _ := sql.Open("pgx", "postgres://localhost:1/nonexistent?connect_timeout=1")
	return service.NewOrgService(store.NewOrgStore(db), store.NewRepoStore(db), store.NewUserStore(db), config.GitConfig{})
}

func TestOrgChecks_AdminModeWidensOnlyTheBoundUser(t *testing.T) {
	svc := newUnreachableOrgSvc()
	const orgID = 9

	ctx, st := adminModeCtx(adminModeUser)
	if !svc.IsOwner(ctx, orgID, adminModeUser) || !svc.IsMember(ctx, orgID, adminModeUser) {
		t.Error("the admin-mode user must pass IsOwner and IsMember")
	}
	if !st.Used() {
		t.Error("the override must be recorded")
	}

	ctx, st = adminModeCtx(adminModeUser)
	if svc.IsOwner(ctx, orgID, 8) || svc.IsMember(ctx, orgID, 8) {
		t.Error("another user's check in the same request must not be widened")
	}
	if st.Used() {
		t.Error("checks for another user must not record an override")
	}

	if svc.IsOwner(context.Background(), orgID, adminModeUser) || svc.IsMember(context.Background(), orgID, adminModeUser) {
		t.Error("checks must fail outside admin mode")
	}
}
