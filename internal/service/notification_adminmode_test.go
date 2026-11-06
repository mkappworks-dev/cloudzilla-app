package service_test

import (
	"sync"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// A superadmin in admin mode comments on a private repo's issue. Notifications
// fan out from the same context, in a goroutine as the handlers do, and each
// recipient is checked for read access on their own.
func TestNotification_AdminModeDoesNotWidenARecipient(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	strangerID := testutil.SeedUser(t, db, "stranger_"+suffix)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE id = $1`, repoID)

	svc := service.NewNotificationService(store.NewNotificationStore(db), store.NewWatchStore(db), newPermSvcDB(db),
		service.NewEmailService(config.SMTPConfig{}),
		service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}))
	repo := model.Repository{ID: repoID, Name: "testrepo_" + suffix, OwnerName: ownerName, OwnerID: ownerID, Private: true}
	issue := model.Issue{ID: 3, Number: 3, AuthorID: strangerID}

	ctx, st := adminModeCtx(adminID)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		svc.NotifyIssueComment(ctx, repo, issue, adminID, "testadmin_"+suffix)
	}()
	wg.Wait()

	if n, _ := svc.CountUnread(ctx, strangerID); n != 0 {
		t.Errorf("a recipient without read access got %d notifications because the actor was in admin mode", n)
	}
	if st.Used() {
		t.Error("a check for another user must not record an admin-mode override")
	}
}
