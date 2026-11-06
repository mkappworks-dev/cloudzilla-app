package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/adminmode"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const adminModeUser = int64(7)

func adminModeCtx(userID int64) (context.Context, *adminmode.State) {
	return adminmode.With(context.Background(), userID, time.Now().Add(time.Hour))
}

func repoChecks(svc interface {
	CanRead(context.Context, *model.Repository, *int64) bool
	CanWrite(context.Context, *model.Repository, int64) bool
	CanManage(context.Context, *model.Repository, int64) bool
	IsOwner(context.Context, *model.Repository, int64) bool
}, ctx context.Context, repo *model.Repository, uid int64) map[string]bool {
	return map[string]bool{
		"CanRead":   svc.CanRead(ctx, repo, &uid),
		"CanWrite":  svc.CanWrite(ctx, repo, uid),
		"CanManage": svc.CanManage(ctx, repo, uid),
		"IsOwner":   svc.IsOwner(ctx, repo, uid),
	}
}

func TestRepoChecks_AdminModeWidensOnlyTheBoundUser(t *testing.T) {
	svc := newPermSvc(nil)
	for name, repo := range map[string]*model.Repository{
		"personal": {Private: true, OwnerID: 42},
		"org":      {Private: true, OrgID: 9},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, st := adminModeCtx(adminModeUser)

			for check, ok := range repoChecks(svc, ctx, repo, adminModeUser) {
				if !ok {
					t.Errorf("%s must pass for the admin-mode user", check)
				}
			}
			if !st.Used() {
				t.Error("the override must be recorded")
			}

			ctx, st = adminModeCtx(adminModeUser)
			for check, ok := range repoChecks(svc, ctx, repo, 8) {
				if ok {
					t.Errorf("%s must not pass for a recipient other than the admin-mode user", check)
				}
			}
			if st.Used() {
				t.Error("checks for another user must not record an override")
			}

			for check, ok := range repoChecks(svc, context.Background(), repo, adminModeUser) {
				if ok {
					t.Errorf("%s must fail outside admin mode", check)
				}
			}
		})
	}
}

func TestRepoChecks_AdminModeNotUsedWhenOrdinaryCheckPasses(t *testing.T) {
	svc := newPermSvc(nil)
	ctx, st := adminModeCtx(42)
	repo := &model.Repository{Private: true, OwnerID: 42}
	for check, ok := range repoChecks(svc, ctx, repo, 42) {
		if !ok {
			t.Errorf("%s must pass for the owner", check)
		}
	}
	if st.Used() {
		t.Error("an owner's own repo must not count as an admin-mode action")
	}
}

func TestCanRead_PublicRepoDoesNotRecordAnOverride(t *testing.T) {
	svc := newPermSvc(nil)
	ctx, st := adminModeCtx(adminModeUser)
	uid := adminModeUser
	if !svc.CanRead(ctx, &model.Repository{OwnerID: 42}, &uid) || st.Used() {
		t.Error("a public repo reads without admin mode and records nothing")
	}
}
