package service_test

import (
	"context"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type keyServices struct {
	ssh    *service.SSHKeyService
	deploy *service.DeployKeyService
	userID int64
	repoID int64
}

func newKeyServices(t *testing.T) keyServices {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, userID, "testuser_"+suffix, suffix)
	sshKeys, deployKeys := store.NewSSHKeyStore(db), store.NewDeployKeyStore(db)
	return keyServices{
		ssh:    service.NewSSHKeyService(sshKeys, store.NewUserStore(db), deployKeys),
		deploy: service.NewDeployKeyService(deployKeys, sshKeys),
		userID: userID,
		repoID: repoID,
	}
}

func TestSSHKeyService_AddKey_RefusesDeployKey(t *testing.T) {
	s := newKeyServices(t)
	ctx := context.Background()
	rawKey := generateTestPublicKey(t)

	if _, err := s.deploy.Add(ctx, s.repoID, "CI", rawKey, true); err != nil {
		t.Fatalf("deploy Add: %v", err)
	}

	_, err := s.ssh.AddKey(ctx, s.userID, "laptop", rawKey)
	if err == nil || err.Error() != "this key is already registered as a deploy key" {
		t.Fatalf("AddKey: want deploy-key collision error, got %v", err)
	}

	keys, err := s.ssh.ListByUser(ctx, s.userID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("refused AddKey must not create an ssh_keys row, got %d", len(keys))
	}

	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(rawKey))
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if _, err := s.ssh.AuthenticatePublicKey(ctx, pub); err == nil {
		t.Error("the key must not authenticate as a user key")
	}
}

func TestDeployKeyService_Add_RefusesUserSSHKey(t *testing.T) {
	s := newKeyServices(t)
	ctx := context.Background()
	rawKey := generateTestPublicKey(t)

	if _, err := s.ssh.AddKey(ctx, s.userID, "laptop", rawKey); err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	_, err := s.deploy.Add(ctx, s.repoID, "CI", rawKey, true)
	if err == nil || err.Error() != "this key is already registered as a user SSH key" {
		t.Fatalf("deploy Add: want user-key collision error, got %v", err)
	}

	keys, err := s.deploy.List(ctx, s.repoID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("refused Add must not create a deploy_keys row, got %d", len(keys))
	}
}
