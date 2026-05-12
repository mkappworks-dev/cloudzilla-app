package service_test

// Integration tests for DeployKeyService. All tests require TEST_DATABASE_DSN and skip otherwise.
// Deploy key tests use a real Ed25519 key pair generated in-memory; no external key files needed.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newDeployKeySvc builds a DeployKeyService backed by the test database and seeds
// an owner + repo. Returns the service and repoID.
func newDeployKeySvc(t *testing.T) (*service.DeployKeyService, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	svc := service.NewDeployKeyService(store.NewDeployKeyStore(db), store.NewSSHKeyStore(db))
	return svc, repoID
}

// generateTestPublicKey returns a random Ed25519 public key formatted as an
// OpenSSH authorized_keys entry (e.g. "ssh-ed25519 AAAA... test-key").
func generateTestPublicKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("convert to ssh key: %v", err)
	}
	return string(gossh.MarshalAuthorizedKey(sshPub))
}

// TestDeployKeyService_Add_AssignsID verifies that Add parses a valid public key,
// inserts the deploy key, and returns it with a non-zero ID.
func TestDeployKeyService_Add_AssignsID(t *testing.T) {
	svc, repoID := newDeployKeySvc(t)

	rawKey := generateTestPublicKey(t)
	key, err := svc.Add(context.Background(), repoID, "CI Deploy Key", rawKey, true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if key.ID == 0 {
		t.Error("Add must return a deploy key with non-zero ID")
	}
	if key.Title != "CI Deploy Key" {
		t.Errorf("want title %q, got %q", "CI Deploy Key", key.Title)
	}
}

// TestDeployKeyService_List_ReturnsAddedKey verifies that List returns the deploy key
// we just added to the repository.
func TestDeployKeyService_List_ReturnsAddedKey(t *testing.T) {
	svc, repoID := newDeployKeySvc(t)

	rawKey := generateTestPublicKey(t)
	if _, err := svc.Add(context.Background(), repoID, "List Key", rawKey, false); err != nil {
		t.Fatalf("Add: %v", err)
	}

	keys, err := svc.List(context.Background(), repoID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) == 0 {
		t.Error("List must return at least the key we added")
	}
}

// TestDeployKeyService_Delete_RemovesKey verifies that Delete removes the deploy key
// so it no longer appears in List.
func TestDeployKeyService_Delete_RemovesKey(t *testing.T) {
	svc, repoID := newDeployKeySvc(t)

	rawKey := generateTestPublicKey(t)
	key, err := svc.Add(context.Background(), repoID, "Delete Key", rawKey, true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := svc.Delete(context.Background(), key.ID, repoID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	keys, _ := svc.List(context.Background(), repoID)
	for _, k := range keys {
		if k.ID == key.ID {
			t.Error("deleted deploy key must not appear in List")
		}
	}
}

// TestDeployKeyService_Add_ReadOnly_StoresFlag verifies that a read-only deploy key
// has its ReadOnly flag set to true in the stored record.
func TestDeployKeyService_Add_ReadOnly_StoresFlag(t *testing.T) {
	svc, repoID := newDeployKeySvc(t)

	rawKey := generateTestPublicKey(t)
	key, err := svc.Add(context.Background(), repoID, "ReadOnly Key", rawKey, true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !key.ReadOnly {
		t.Error("Add with readOnly=true must store ReadOnly=true")
	}
}
