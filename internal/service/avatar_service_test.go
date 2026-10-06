package service_test

// Integration tests for AvatarService. They require TEST_DATABASE_DSN and skip otherwise.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/avatar"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type avatarEnv struct {
	svcs    *service.Services
	backend storage.Backend
	users   *store.UserStore
}

func newAvatarEnv(t *testing.T) avatarEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	backend, err := storage.NewLocal(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Git: config.GitConfig{ReposRoot: t.TempDir()}}
	svcs := service.New(store.New(db), cfg).WithStorage(backend)
	return avatarEnv{svcs: svcs, backend: backend, users: store.NewUserStore(db)}
}

func pngOf(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (e avatarEnv) exists(t *testing.T, key string) bool {
	t.Helper()
	ok, err := e.backend.Exists(context.Background(), key)
	if err != nil {
		t.Fatalf("Exists(%q): %v", key, err)
	}
	return ok
}

func (e avatarEnv) userKey(t *testing.T, id int64) string {
	t.Helper()
	u, err := e.users.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return u.AvatarKey
}

func TestAvatarService_UserUploadReplaceRemove(t *testing.T) {
	env := newAvatarEnv(t)
	ctx := context.Background()
	userID := testutil.SeedUser(t, testutil.OpenTestDB(t), testutil.UniqueSuffix(t))

	first, err := env.svcs.Avatar.SetUserAvatar(ctx, userID, bytes.NewReader(pngOf(t, color.NRGBA{255, 0, 0, 255})))
	if err != nil {
		t.Fatalf("SetUserAvatar: %v", err)
	}
	if want := fmt.Sprintf("avatars/user/%d/", userID); len(first) < len(want) || first[:len(want)] != want {
		t.Errorf("key %q lacks prefix %q", first, want)
	}
	if !env.exists(t, first) || env.userKey(t, userID) != first {
		t.Fatal("upload did not store the object and set the key")
	}

	again, err := env.svcs.Avatar.SetUserAvatar(ctx, userID, bytes.NewReader(pngOf(t, color.NRGBA{255, 0, 0, 255})))
	if err != nil {
		t.Fatalf("SetUserAvatar same image: %v", err)
	}
	if again != first || !env.exists(t, first) {
		t.Error("re-uploading the same image changed the key or deleted the object")
	}

	second, err := env.svcs.Avatar.SetUserAvatar(ctx, userID, bytes.NewReader(pngOf(t, color.NRGBA{0, 0, 255, 255})))
	if err != nil {
		t.Fatalf("SetUserAvatar replace: %v", err)
	}
	if second == first || env.exists(t, first) || !env.exists(t, second) {
		t.Error("replace did not swap to a new object and delete the old one")
	}

	if err := env.svcs.Avatar.RemoveUserAvatar(ctx, userID); err != nil {
		t.Fatalf("RemoveUserAvatar: %v", err)
	}
	if env.exists(t, second) || env.userKey(t, userID) != "" {
		t.Error("remove left the object or the key")
	}
}

func TestAvatarService_RejectsBadImageWithoutWriting(t *testing.T) {
	env := newAvatarEnv(t)
	userID := testutil.SeedUser(t, testutil.OpenTestDB(t), testutil.UniqueSuffix(t))
	_, err := env.svcs.Avatar.SetUserAvatar(context.Background(), userID, bytes.NewReader([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)))
	if !errors.Is(err, avatar.ErrUnsupportedType) {
		t.Fatalf("err = %v, want ErrUnsupportedType", err)
	}
	if env.userKey(t, userID) != "" {
		t.Error("a rejected upload set the key")
	}
}

func TestAvatarService_FailedSwapDeletesNewObject(t *testing.T) {
	env := newAvatarEnv(t)
	data := pngOf(t, color.NRGBA{9, 9, 9, 255})
	img, err := avatar.Process(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	const missingUser = int64(1) << 60
	if _, err := env.svcs.Avatar.SetUserAvatar(context.Background(), missingUser, bytes.NewReader(data)); err == nil {
		t.Fatal("SetUserAvatar for a missing user succeeded")
	}
	if key := fmt.Sprintf("avatars/user/%d/%s.%s", missingUser, img.SHA256, img.Ext); env.exists(t, key) {
		t.Error("the new object outlived the failed key update")
	}
}

func TestAvatarService_DeleteUserRemovesObject(t *testing.T) {
	env := newAvatarEnv(t)
	ctx := context.Background()
	userID := testutil.SeedUser(t, testutil.OpenTestDB(t), testutil.UniqueSuffix(t))
	key, err := env.svcs.Avatar.SetUserAvatar(ctx, userID, bytes.NewReader(pngOf(t, color.White)))
	if err != nil {
		t.Fatal(err)
	}
	if err := env.svcs.User.DeleteUser(ctx, userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if env.exists(t, key) {
		t.Error("deleting the user left the avatar object")
	}
}

func TestAvatarService_OrgOwnerOnlyAndDeleteRemovesObject(t *testing.T) {
	env := newAvatarEnv(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, "owner_"+suffix)
	memberID := testutil.SeedUser(t, db, "member_"+suffix)
	org, err := env.svcs.Org.Create(ctx, ownerID, "org-"+suffix, "Org", "")
	if err != nil {
		t.Fatalf("Create org: %v", err)
	}
	if err := env.svcs.Org.AddMember(ctx, org.ID, ownerID, memberID, "member"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if _, err := env.svcs.Avatar.SetOrgAvatar(ctx, org.ID, memberID, bytes.NewReader(pngOf(t, color.White))); !errors.Is(err, service.ErrNotOrgOwner) {
		t.Errorf("member SetOrgAvatar: err = %v, want ErrNotOrgOwner", err)
	}
	key, err := env.svcs.Avatar.SetOrgAvatar(ctx, org.ID, ownerID, bytes.NewReader(pngOf(t, color.White)))
	if err != nil {
		t.Fatalf("owner SetOrgAvatar: %v", err)
	}
	if want := fmt.Sprintf("avatars/org/%d/", org.ID); key[:len(want)] != want || !env.exists(t, key) {
		t.Fatalf("org key %q not stored under %q", key, want)
	}
	if err := env.svcs.Avatar.RemoveOrgAvatar(ctx, org.ID, memberID); !errors.Is(err, service.ErrNotOrgOwner) {
		t.Errorf("member RemoveOrgAvatar: err = %v, want ErrNotOrgOwner", err)
	}

	if err := env.svcs.Org.Delete(ctx, org.ID, ownerID); err != nil {
		t.Fatalf("Delete org: %v", err)
	}
	if env.exists(t, key) {
		t.Error("deleting the org left the avatar object")
	}
}

// A remove racing an upload of the same image must not delete the object the
// row ends up pointing at: both use the same content-hash key.
func TestAvatarService_ConcurrentRemoveAndReuploadKeepObject(t *testing.T) {
	env := newAvatarEnv(t)
	ctx := context.Background()
	userID := testutil.SeedUser(t, testutil.OpenTestDB(t), testutil.UniqueSuffix(t))
	data := pngOf(t, color.NRGBA{77, 0, 0, 255})
	for i := range 25 {
		if _, err := env.svcs.Avatar.SetUserAvatar(ctx, userID, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = env.svcs.Avatar.RemoveUserAvatar(ctx, userID) }()
		go func() { defer wg.Done(); _, _ = env.svcs.Avatar.SetUserAvatar(ctx, userID, bytes.NewReader(data)) }()
		wg.Wait()
		if key := env.userKey(t, userID); key != "" && !env.exists(t, key) {
			t.Fatalf("round %d: avatar_key %q points at a deleted object", i, key)
		}
	}
}
