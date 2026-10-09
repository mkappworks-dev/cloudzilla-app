package service_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type notifAccessEnv struct {
	db        *sql.DB
	suffix    string
	svc       *service.NotificationService
	userSvc   *service.UserService
	repoStore *store.RepoStore
	orgStore  *store.OrgStore
	watches   *store.WatchStore
	threads   *service.ThreadSubscriptionService
	actorID   int64
}

func newNotifAccessEnv(t *testing.T) *notifAccessEnv {
	t.Helper()
	return newNotifAccessEnvWithSMTP(t, config.SMTPConfig{})
}

func newNotifAccessEnvWithSMTP(t *testing.T, smtp config.SMTPConfig) *notifAccessEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	userSvc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	repoStore := store.NewRepoStore(db)
	repoSvc := service.NewRepoService(repoStore, store.NewUserStore(db), store.NewOrgStore(db), nil, nil, config.GitConfig{})
	watches := store.NewWatchStore(db)
	suffix := testutil.UniqueSuffix(t)
	threads := service.NewThreadSubscriptionService(store.NewThreadSubscriptionStore(db), watches)
	return &notifAccessEnv{
		db:        db,
		suffix:    suffix,
		svc:       service.NewNotificationService(store.NewNotificationStore(db), watches, repoSvc, service.NewEmailService(smtp), userSvc).WithThreadSubscriptions(threads),
		threads:   threads,
		userSvc:   userSvc,
		repoStore: repoStore,
		orgStore:  store.NewOrgStore(db),
		watches:   watches,
		actorID:   testutil.SeedUser(t, db, "actor_"+suffix),
	}
}

func (e *notifAccessEnv) seedUser(t *testing.T, name string) int64 {
	t.Helper()
	return testutil.SeedUser(t, e.db, name+"_"+e.suffix)
}

// privateRepo returns a private personal repo owned by ownerID that the actor can write to.
func (e *notifAccessEnv) privateRepo(t *testing.T, ownerID int64) model.Repository {
	t.Helper()
	ctx := context.Background()
	ownerName := "testuser_owner_" + e.suffix
	id := testutil.SeedRepo(t, e.db, ownerID, ownerName, e.suffix)
	testutil.Exec(t, e.db, `UPDATE repositories SET private = true WHERE id = $1`, id)
	if err := e.repoStore.AddPermission(ctx, id, e.actorID, string(model.RoleWriter)); err != nil {
		t.Fatalf("grant actor: %v", err)
	}
	return model.Repository{ID: id, OwnerID: ownerID, OwnerName: ownerName, Name: "testrepo_" + e.suffix, Private: true}
}

func (e *notifAccessEnv) grantReader(t *testing.T, repoID, userID int64) {
	t.Helper()
	if err := e.repoStore.AddPermission(context.Background(), repoID, userID, string(model.RoleReader)); err != nil {
		t.Fatalf("grant reader: %v", err)
	}
}

func (e *notifAccessEnv) revoke(t *testing.T, repoID, userID int64) {
	t.Helper()
	if err := e.repoStore.RemovePermission(context.Background(), repoID, userID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
}

func (e *notifAccessEnv) watch(t *testing.T, repoID int64, userIDs ...int64) {
	t.Helper()
	for _, uid := range userIDs {
		if err := e.watches.Set(context.Background(), uid, repoID, model.WatchLevelWatching); err != nil {
			t.Fatalf("watch: %v", err)
		}
	}
}

func (e *notifAccessEnv) count(t *testing.T, userID int64) int {
	t.Helper()
	n, err := e.svc.CountUnread(context.Background(), userID)
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	return n
}

func TestNotification_FanOut_SkipsWatcherRemovedAsCollaborator(t *testing.T) {
	env := newNotifAccessEnv(t)
	ownerID := env.seedUser(t, "owner")
	readerID := env.seedUser(t, "reader")
	formerID := env.seedUser(t, "former")
	repo := env.privateRepo(t, ownerID)
	env.grantReader(t, repo.ID, readerID)
	env.grantReader(t, repo.ID, formerID)
	env.watch(t, repo.ID, readerID, formerID)
	env.revoke(t, repo.ID, formerID)

	env.svc.NotifyIssueComment(context.Background(), repo, model.Issue{Number: 3, AuthorID: ownerID}, env.actorID, "actor")

	if got := env.count(t, readerID); got != 1 {
		t.Errorf("watcher who can read got %d notifications, want 1", got)
	}
	if got := env.count(t, formerID); got != 0 {
		t.Errorf("watcher removed as collaborator got %d notifications, want 0", got)
	}
}

func TestNotification_FanOut_SkipsWatcherRemovedOrDemotedInOrg(t *testing.T) {
	env := newNotifAccessEnv(t)
	ctx := context.Background()
	keeperID := env.seedUser(t, "keeper")
	demotedID := env.seedUser(t, "demoted")
	removedID := env.seedUser(t, "removed")

	org := &model.Organization{Name: "org_" + env.suffix}
	if err := env.orgStore.Create(ctx, org); err != nil {
		t.Fatalf("create org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, env.db, org.ID)
	for _, uid := range []int64{keeperID, demotedID, removedID} {
		if err := env.orgStore.AddMember(ctx, org.ID, uid, model.OrgRoleOwner); err != nil {
			t.Fatalf("add owner: %v", err)
		}
	}
	var repoID int64
	if err := env.db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_name, org_id, name, description, private) VALUES ($1, $2, $3, '', true) RETURNING id`,
		org.Name, org.ID, "orgrepo_"+env.suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("create org repo: %v", err)
	}
	t.Cleanup(func() {
		testutil.Exec(t, env.db, `DELETE FROM permissions WHERE repo_id = $1`, repoID)
		testutil.Exec(t, env.db, `DELETE FROM repositories WHERE id = $1`, repoID)
	})
	if err := env.repoStore.AddPermission(ctx, repoID, env.actorID, string(model.RoleWriter)); err != nil {
		t.Fatalf("grant actor: %v", err)
	}
	env.watch(t, repoID, keeperID, demotedID, removedID)
	if err := env.orgStore.UpdateMemberRole(ctx, org.ID, demotedID, model.OrgRoleMember); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if err := env.orgStore.RemoveMember(ctx, org.ID, removedID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	repo := model.Repository{ID: repoID, OrgID: org.ID, OwnerName: org.Name, Name: "orgrepo_" + env.suffix, Private: true}
	env.svc.NotifyPRComment(ctx, repo, model.PullRequest{Number: 4, AuthorID: keeperID}, env.actorID, "actor")

	if got := env.count(t, keeperID); got != 1 {
		t.Errorf("org owner got %d notifications, want 1", got)
	}
	if got := env.count(t, demotedID); got != 0 {
		t.Errorf("watcher demoted to org member got %d notifications, want 0", got)
	}
	if got := env.count(t, removedID); got != 0 {
		t.Errorf("watcher removed from org got %d notifications, want 0", got)
	}
}

func TestNotification_DirectRecipientWithoutReadAccess_NotNotified(t *testing.T) {
	env := newNotifAccessEnv(t)
	ownerID := env.seedUser(t, "owner")
	formerID := env.seedUser(t, "former")
	outsiderID := env.seedUser(t, "outsider")
	repo := env.privateRepo(t, ownerID)
	env.grantReader(t, repo.ID, formerID)
	env.revoke(t, repo.ID, formerID)

	ctx := context.Background()
	issue := model.Issue{Number: 1, AuthorID: formerID, State: model.IssueStateClosed}
	pr := model.PullRequest{Number: 2, AuthorID: formerID, State: model.PRStateMerged}
	cases := []struct {
		name      string
		recipient int64
		notify    func()
	}{
		{"issue comment", formerID, func() { env.svc.NotifyIssueComment(ctx, repo, issue, env.actorID, "actor") }},
		{"issue state change", formerID, func() { env.svc.NotifyIssueStateChange(ctx, repo, issue, env.actorID, "actor") }},
		{"pr comment", formerID, func() { env.svc.NotifyPRComment(ctx, repo, pr, env.actorID, "actor") }},
		{"pr review", formerID, func() { env.svc.NotifyPRReview(ctx, repo, pr, env.actorID, "actor") }},
		{"pr state change", formerID, func() { env.svc.NotifyPRStateChange(ctx, repo, pr, env.actorID, "actor") }},
		{"discussion reply", formerID, func() {
			env.svc.NotifyDiscussionReply(ctx, repo, model.Discussion{Number: 5, AuthorID: formerID}, env.actorID, "actor")
		}},
		{"mention", outsiderID, func() {
			env.svc.NotifyMention(ctx, repo, env.actorID, "actor", outsiderID, model.ThreadKindIssue, 1, "/x")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.notify()
			if got := env.count(t, tc.recipient); got != 0 {
				t.Errorf("recipient without read access has %d notifications, want 0", got)
			}
		})
	}
}

func TestNotification_ListUnreadForDigest_SkipsReposNoLongerReadable(t *testing.T) {
	env := newNotifAccessEnv(t)
	ctx := context.Background()
	ownerID := env.seedUser(t, "owner")
	userID := env.seedUser(t, "digest")
	repo := env.privateRepo(t, ownerID)
	env.grantReader(t, repo.ID, userID)
	publicID := testutil.SeedRepo(t, env.db, ownerID, repo.OwnerName, "public_"+env.suffix)
	public := model.Repository{ID: publicID, OwnerID: ownerID, OwnerName: repo.OwnerName, Name: "testrepo_public_" + env.suffix}

	prefs := model.NotificationPrefs{EmailNotifications: true, EmailDigest: model.EmailDigestDaily, NotifyPRReview: true, NotifyMention: true}
	if err := env.userSvc.UpdateNotificationPrefs(ctx, userID, prefs); err != nil {
		t.Fatalf("UpdateNotificationPrefs: %v", err)
	}
	env.svc.NotifyIssueComment(ctx, repo, model.Issue{Number: 1, AuthorID: userID}, env.actorID, "actor")
	env.svc.NotifyIssueComment(ctx, public, model.Issue{Number: 2, AuthorID: userID}, env.actorID, "actor")
	if got := env.count(t, userID); got != 2 {
		t.Fatalf("seeded %d notifications, want 2", got)
	}
	env.revoke(t, repo.ID, userID)

	u, err := env.userSvc.GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	got, err := env.svc.ListUnreadForDigest(ctx, u, model.EmailDigestDaily)
	if err != nil {
		t.Fatalf("ListUnreadForDigest: %v", err)
	}
	if len(got) != 1 || got[0].RepoID != publicID {
		t.Errorf("digest = %+v, want only the public repo's notification", got)
	}
}
