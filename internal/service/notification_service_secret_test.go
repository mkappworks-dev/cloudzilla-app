package service_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type secretNotifFixture struct {
	env                                 *notifAccessEnv
	repo                                model.Repository
	owner, orgOwner, adminCollab        int64
	writer, teamAdmin, teamWriter, memb int64
}

// orgRepoFixture builds an org repo with an org owner, an admin collaborator, a writer, a member, and one team
// granted admin and one granted write.
func orgRepoFixture(t *testing.T) secretNotifFixture {
	t.Helper()
	e := newNotifAccessEnv(t)
	db := e.db
	f := secretNotifFixture{env: e}
	f.owner = e.seedUser(t, "creator")
	f.orgOwner = e.seedUser(t, "orgowner")
	f.adminCollab = e.seedUser(t, "admincollab")
	f.writer = e.seedUser(t, "writer")
	f.teamAdmin = e.seedUser(t, "teamadmin")
	f.teamWriter = e.seedUser(t, "teamwriter")
	f.memb = e.seedUser(t, "member")

	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, "org_"+e.suffix).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	repoID := testutil.SeedRepo(t, db, f.owner, "org_"+e.suffix, e.suffix)
	testutil.Exec(t, db, `UPDATE repositories SET org_id = $1, owner_id = NULL, created_by = $3 WHERE id = $2`, orgID, repoID, f.owner)
	testutil.Exec(t, db, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1,$2,'owner'),($1,$3,'member')`, orgID, f.orgOwner, f.memb)
	ctx := context.Background()
	if err := e.repoStore.AddPermission(ctx, repoID, f.adminCollab, string(model.RoleAdmin)); err != nil {
		t.Fatal(err)
	}
	if err := e.repoStore.AddPermission(ctx, repoID, f.writer, string(model.RoleWriter)); err != nil {
		t.Fatal(err)
	}
	for _, g := range []struct {
		slug string
		user int64
		role string
	}{{"admins", f.teamAdmin, "admin"}, {"writers", f.teamWriter, "writer"}} {
		var teamID int64
		if err := db.QueryRow(`INSERT INTO teams (org_id, name, slug) VALUES ($1,$2,$2) RETURNING id`, orgID, g.slug).Scan(&teamID); err != nil {
			t.Fatal(err)
		}
		testutil.Exec(t, db, `INSERT INTO team_members (team_id, user_id) VALUES ($1,$2)`, teamID, g.user)
		testutil.Exec(t, db, `INSERT INTO team_repos (team_id, repo_id, role) VALUES ($1,$2,$3)`, teamID, repoID, g.role)
	}
	f.repo = model.Repository{ID: repoID, OrgID: orgID, OwnerName: "org_" + e.suffix, Name: "testrepo_" + e.suffix}
	return f
}

func (f secretNotifFixture) notified(t *testing.T, userID int64, typ model.NotificationType) []model.Notification {
	t.Helper()
	all, err := f.env.svc.List(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(all, func(n model.Notification) bool { return n.Type != typ })
}

func TestNotifySecretAlert_NotifiesExactlyTheAdmins(t *testing.T) {
	f := orgRepoFixture(t)
	f.env.svc.NotifySecretAlert(context.Background(), f.repo, &model.SecretScanAlert{Number: 1, SecretType: "aws_access_key"}, 1)

	for name, uid := range map[string]int64{"org owner": f.orgOwner, "admin collaborator": f.adminCollab, "team admin": f.teamAdmin} {
		if got := len(f.notified(t, uid, model.NotifSecretAlert)); got != 1 {
			t.Errorf("%s got %d notifications, want 1", name, got)
		}
	}
	for name, uid := range map[string]int64{"repo creator": f.owner, "writer": f.writer, "team writer": f.teamWriter, "member": f.memb} {
		if got := len(f.notified(t, uid, model.NotifSecretAlert)); got != 0 {
			t.Errorf("%s got %d notifications, want 0", name, got)
		}
	}
}

func TestNotifySecretAlert_PersonalRepoNotifiesOwnerAndAdminCollaborator(t *testing.T) {
	e := newNotifAccessEnv(t)
	owner, admin, writer := e.seedUser(t, "owner"), e.seedUser(t, "admin"), e.seedUser(t, "writer")
	repoID := testutil.SeedRepo(t, e.db, owner, "owner_"+e.suffix, e.suffix)
	ctx := context.Background()
	_ = e.repoStore.AddPermission(ctx, repoID, admin, string(model.RoleAdmin))
	_ = e.repoStore.AddPermission(ctx, repoID, writer, string(model.RoleWriter))
	repo := model.Repository{ID: repoID, OwnerID: owner, OwnerName: "owner_" + e.suffix, Name: "testrepo_" + e.suffix}

	e.svc.NotifySecretAlert(ctx, repo, &model.SecretScanAlert{Number: 1, SecretType: "x"}, 1)

	f := secretNotifFixture{env: e}
	if len(f.notified(t, owner, model.NotifSecretAlert)) != 1 || len(f.notified(t, admin, model.NotifSecretAlert)) != 1 {
		t.Error("owner and admin collaborator should be notified")
	}
	if len(f.notified(t, writer, model.NotifSecretAlert)) != 0 {
		t.Error("writer should not be notified")
	}
}

func TestNotifySecretAlert_DigestIsOneNotification(t *testing.T) {
	f := orgRepoFixture(t)
	f.env.svc.NotifySecretAlert(context.Background(), f.repo, &model.SecretScanAlert{Number: 1, SecretType: "x"}, 7)

	got := f.notified(t, f.orgOwner, model.NotifSecretAlert)
	if len(got) != 1 {
		t.Fatalf("got %d notifications, want 1", len(got))
	}
	if got[0].SubjectTitle != "7 secrets" || !strings.HasSuffix(got[0].SubjectURL, "/security/secrets") {
		t.Errorf("digest = %+v", got[0])
	}
}

func TestNotifySecretAllowed_SkipsTheActor(t *testing.T) {
	f := orgRepoFixture(t)
	actor := model.User{ID: f.adminCollab, Username: "admincollab"}
	f.env.svc.NotifySecretAllowed(context.Background(), f.repo, &model.SecretScanAlert{Number: 3, SecretType: "x"}, actor)

	if got := len(f.notified(t, f.adminCollab, model.NotifSecretAllowed)); got != 0 {
		t.Errorf("actor got %d notifications, want 0", got)
	}
	if got := f.notified(t, f.orgOwner, model.NotifSecretAllowed); len(got) != 1 || got[0].ActorName != "admincollab" {
		t.Errorf("org owner notifications = %+v", got)
	}
}

func TestNotifyTokenRevoked_TellsTheOwner(t *testing.T) {
	f := orgRepoFixture(t)
	f.env.svc.NotifyTokenRevoked(context.Background(), f.teamWriter, model.AccessToken{ID: 9, Name: "ci"}, f.repo)

	got := f.notified(t, f.teamWriter, model.NotifTokenRevoked)
	if len(got) != 1 || got[0].SubjectTitle != "ci" {
		t.Errorf("notifications = %+v", got)
	}
	if len(f.notified(t, f.orgOwner, model.NotifTokenRevoked)) != 0 {
		t.Error("only the token owner is told")
	}
}

func TestSecretScanAudit_OmitsTheHint(t *testing.T) {
	name, meta := service.SecretScanAudit(model.Repository{OwnerName: "o", Name: "r"}, &model.SecretScanAlert{Number: 4, SecretType: "t", SecretHint: "AKIA****"}, "revoked")
	if name != "o/r#4" || meta["resolution"] != "revoked" {
		t.Errorf("name=%q meta=%v", name, meta)
	}
	for _, v := range meta {
		if s, ok := v.(string); ok && strings.Contains(s, "AKIA") {
			t.Errorf("hint leaked: %v", meta)
		}
	}
}
