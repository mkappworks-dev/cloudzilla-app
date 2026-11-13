package service_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestMirrorSync_QueuesSecretScan(t *testing.T) {
	e := newMirrorEnv(t, true)
	ctx := context.Background()
	m := e.mirror(t, testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", ""), nil)
	testutil.Exec(t, e.db, `UPDATE repo_secret_scans SET state = 'done' WHERE repo_id = $1`, e.repoID)

	if err := e.svc.Mirror.Sync(ctx, m); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if state, _, _ := e.svc.SecretScan.ScanState(ctx, e.repoID); state != "pending" {
		t.Errorf("scan state after a sync that moved refs = %q, want pending", state)
	}
}

func TestImport_QueuesSecretScan(t *testing.T) {
	svc, _, uid, uname := newMirrorImportEnv(t, mirrorsOn, "")
	ctx := context.Background()
	job, err := svc.Import.Start(ctx, uid, uname, service.ImportRequest{
		CloneURL: testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", ""), Name: "scanme",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitImport(t, svc.Import, uid, job.ID)
	repo, err := svc.Repo.Get(ctx, uname, "scanme")
	if err != nil {
		t.Fatal(err)
	}
	if state, _, _ := svc.SecretScan.ScanState(ctx, repo.ID); state != "pending" {
		t.Errorf("scan state after an import = %q, want pending", state)
	}
}
