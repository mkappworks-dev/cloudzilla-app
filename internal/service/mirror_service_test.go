package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type mirrorEnv struct {
	svc     *service.Services
	db      *sql.DB
	root    string
	repoID  int64
	ownerID int64
	git     *gogit.Repository
}

func newMirrorEnv(t *testing.T, allowLocal bool) mirrorEnv {
	t.Helper()
	return newMirrorEnvOn(t, testutil.OpenTestDB(t), allowLocal, 1)
}

func newMirrorEnvOn(t *testing.T, db *sql.DB, allowLocal bool, maxConcurrent int) mirrorEnv {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		Git:      config.GitConfig{ReposRoot: root},
		Webhook:  config.WebhookConfig{AllowLocalNetworks: true},
		Security: config.SecurityConfig{SecretKey: strings.Repeat("k", 32)},
		Mirror: config.MirrorConfig{Enabled: true, AllowLocalNetworks: allowLocal, MinInterval: time.Minute,
			DefaultInterval: time.Hour, MaxConcurrent: maxConcurrent, Timeout: time.Minute},
	}
	e := mirrorEnv{svc: service.New(store.New(db), cfg), db: db, root: root}
	e.repoID, e.ownerID, e.git = e.addRepo(t)
	return e
}

// addRepo seeds a repo, under a new owner, with an empty bare repo on disk.
func (e mirrorEnv) addRepo(t *testing.T) (repoID, ownerID int64, git *gogit.Repository) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	ownerID = testutil.SeedUser(t, e.db, suffix)
	owner, name := "testuser_"+suffix, "testrepo_"+suffix
	repoID = testutil.SeedRepo(t, e.db, ownerID, owner, suffix)
	git, err := gogit.PlainInit(filepath.Join(e.root, owner, name+".git"), true)
	if err != nil {
		t.Fatalf("init mirror repo: %v", err)
	}
	return repoID, ownerID, git
}

func (e mirrorEnv) mirror(t *testing.T, url string, token []byte) *model.RepoMirror {
	t.Helper()
	ms := store.NewMirrorStore(e.db)
	m := &model.RepoMirror{RepoID: e.repoID, RemoteURL: url, AuthUsername: "alice", AuthTokenEnc: token,
		Interval: time.Hour, NextSyncAt: time.Now(), CreatedBy: e.ownerID}
	if err := ms.Create(context.Background(), m); err != nil {
		t.Fatalf("create mirror: %v", err)
	}
	got, err := ms.Get(context.Background(), e.repoID)
	if err != nil {
		t.Fatalf("get mirror: %v", err)
	}
	return got
}

// refs maps every branch and tag, plus any other ref, to its hash; HEAD to its target.
func refs(t *testing.T, git *gogit.Repository) map[string]string {
	t.Helper()
	iter, err := git.Storer.IterReferences()
	if err != nil {
		t.Fatalf("iterate refs: %v", err)
	}
	got := map[string]string{}
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		got[r.Name().String()] = r.Strings()[1]
		return nil
	})
	return got
}

func setRef(t *testing.T, st storer.ReferenceStorer, name plumbing.ReferenceName, h plumbing.Hash) {
	t.Helper()
	if err := st.SetReference(plumbing.NewHashReference(name, h)); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
}

func (e mirrorEnv) defaultBranch(t *testing.T) string {
	t.Helper()
	var b string
	if err := e.db.QueryRow(`SELECT default_branch FROM repositories WHERE id = $1`, e.repoID).Scan(&b); err != nil {
		t.Fatalf("read default branch: %v", err)
	}
	return b
}

func TestMirrorSync_FollowsUpstream(t *testing.T) {
	e := newMirrorEnv(t, true)
	ctx := context.Background()
	src := testutil.SeedSourceRepo(t)
	m := e.mirror(t, testutil.ServeGitHTTP(t, src.Dir, "", ""), nil)

	if err := e.svc.Mirror.Sync(ctx, m); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	want := map[string]string{
		"HEAD":               "ref: refs/heads/develop",
		"refs/heads/main":    src.First.String(),
		"refs/heads/develop": src.Second.String(),
		"refs/tags/v1":       src.First.String(),
	}
	if got := refs(t, e.git); !mapsEqual(got, want) {
		t.Errorf("after first sync refs = %v, want %v", got, want)
	}
	if b := e.defaultBranch(t); b != "develop" {
		t.Errorf("default_branch = %q, want develop", b)
	}

	upstream, err := gogit.PlainOpen(src.Dir)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	rewritten := testutil.WriteCommit(t, upstream.Storer, "rewritten")
	setRef(t, upstream.Storer, plumbing.NewBranchReferenceName("main"), rewritten)
	setRef(t, upstream.Storer, plumbing.NewBranchReferenceName("feature"), src.First)
	setRef(t, upstream.Storer, plumbing.NewTagReferenceName("v2"), rewritten)
	for _, name := range []plumbing.ReferenceName{plumbing.NewBranchReferenceName("develop"), plumbing.NewTagReferenceName("v1")} {
		if err := upstream.Storer.RemoveReference(name); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
	}
	if err := upstream.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		t.Fatalf("set HEAD: %v", err)
	}

	if err := e.svc.Mirror.Sync(ctx, m); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	want = map[string]string{
		"HEAD":               "ref: refs/heads/main",
		"refs/heads/main":    rewritten.String(),
		"refs/heads/feature": src.First.String(),
		"refs/tags/v2":       rewritten.String(),
	}
	if got := refs(t, e.git); !mapsEqual(got, want) {
		t.Errorf("after second sync refs = %v, want %v", got, want)
	}
	if b := e.defaultBranch(t); b != "main" {
		t.Errorf("default_branch = %q, want main", b)
	}
}

// go-git's client strips thin-pack from what it asks for, so a fetch only
// ever gets self-contained packs and can keep the storer's PackfileWriter
// fast path. A push can't: see gittransport.WrapForReceive.
func TestMirrorSync_GoGitNeverRequestsThinPacks(t *testing.T) {
	if !slices.Contains(transport.UnsupportedCapabilities, capability.ThinPack) {
		t.Fatal("go-git may now request thin packs: fetch through a storer without PackfileWriter, as gittransport.WrapForReceive does for pushes")
	}
}

func TestMirrorSync_IncrementalFetch(t *testing.T) {
	e := newMirrorEnv(t, true)
	ctx := context.Background()
	srcDir := t.TempDir()
	upstream, err := gogit.PlainInit(srcDir, true)
	if err != nil {
		t.Fatalf("init source: %v", err)
	}
	st := upstream.Storer
	baseContent := []byte("one\ntwo\nthree\n")
	first := writeFileCommit(t, st, writeBlob(t, st, baseContent), "first")
	setRef(t, st, plumbing.NewBranchReferenceName("main"), first)
	m := e.mirror(t, testutil.ServeGitHTTP(t, srcDir, "", ""), nil)
	if err := e.svc.Mirror.Sync(ctx, m); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	nextContent := append(append([]byte(nil), baseContent...), "four\n"...)
	next := writeBlob(t, st, nextContent)
	second := writeFileCommit(t, st, next, "second", first)
	setRef(t, st, plumbing.NewBranchReferenceName("main"), second)

	if err := e.svc.Mirror.Sync(ctx, m); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if got := refs(t, e.git)["refs/heads/main"]; got != second.String() {
		t.Errorf("main = %s, want %s", got, second)
	}
	blob, err := e.git.BlobObject(next)
	if err != nil {
		t.Fatalf("fetched blob missing: %v", err)
	}
	r, _ := blob.Reader()
	got, _ := io.ReadAll(r)
	if string(got) != string(nextContent) {
		t.Errorf("fetched blob = %q, want %q", got, nextContent)
	}
}

func TestMirrorSync_PrivateNetworkRefused(t *testing.T) {
	e := newMirrorEnv(t, false)
	src := testutil.SeedSourceRepo(t)
	m := e.mirror(t, testutil.ServeGitHTTP(t, src.Dir, "", ""), nil)

	err := e.svc.Mirror.Sync(context.Background(), m)

	if err == nil || !strings.Contains(err.Error(), "mirror.allow_local_networks") {
		t.Errorf("Sync err = %v, want one naming mirror.allow_local_networks", err)
	}
	if got := refs(t, e.git); len(got) > 1 {
		t.Errorf("refs = %v, want nothing fetched", got)
	}
}

func TestMirrorSync_StoredCredentials(t *testing.T) {
	src := testutil.SeedSourceRepo(t)
	url := testutil.ServeGitHTTP(t, src.Dir, "alice", "s3cret")
	for _, tc := range []struct {
		name    string
		token   func(e mirrorEnv) []byte
		wantErr string
	}{
		{"right token", func(e mirrorEnv) []byte { return seal(t, e, "s3cret") }, ""},
		{"wrong token", func(e mirrorEnv) []byte { return seal(t, e, "wr0ng-t0ken") }, "the stored credentials were rejected"},
		{"no token", func(mirrorEnv) []byte { return nil }, "the stored credentials were rejected"},
		{"undecryptable", func(mirrorEnv) []byte { return []byte{1, 2, 3} }, "re-enter the token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newMirrorEnv(t, true)
			err := e.svc.Mirror.Sync(context.Background(), e.mirror(t, url, tc.token(e)))
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("Sync: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("Sync err = %v, want it to contain %q", err, tc.wantErr)
			case err != nil && (strings.Contains(err.Error(), "wr0ng") || strings.Contains(err.Error(), "s3cret")):
				t.Errorf("Sync err %q leaks the token", err)
			}
		})
	}
}

func seal(t *testing.T, e mirrorEnv, token string) []byte {
	t.Helper()
	sealed, err := e.svc.Mirror.SealToken(token)
	if err != nil {
		t.Fatalf("SealToken: %v", err)
	}
	return sealed
}

func TestMirrorSync_SideEffects(t *testing.T) {
	e := newMirrorEnv(t, true)
	ctx := context.Background()
	src := testutil.SeedSourceRepo(t)
	m := e.mirror(t, testutil.ServeGitHTTP(t, src.Dir, "", ""), nil)
	pushes := make(chan map[string]any, 10)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		pushes <- p
	}))
	t.Cleanup(hook.Close)
	if _, err := e.svc.Webhook.Create(ctx, e.repoID, hook.URL, "", "push"); err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	var pullID int64
	if err := e.db.QueryRow(`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		VALUES ($1, 1, $2, 'change', 'open', 'develop', 'main') RETURNING id`, e.repoID, e.ownerID).Scan(&pullID); err != nil {
		t.Fatalf("seed pull: %v", err)
	}

	if err := e.svc.Mirror.Sync(ctx, m); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	got := map[string]string{}
	for range 2 {
		select {
		case p := <-pushes:
			pusher, _ := p["pusher"].(map[string]any)
			if name, _ := pusher["name"].(string); name != "" {
				t.Errorf("push pusher = %q, want empty", name)
			}
			ref, _ := p["ref"].(string)
			after, _ := p["after"].(string)
			got[ref] = after
		case <-time.After(5 * time.Second):
			t.Fatalf("push webhooks received: %v, want main and develop", got)
		}
	}
	if want := map[string]string{"refs/heads/main": src.First.String(), "refs/heads/develop": src.Second.String()}; !mapsEqual(got, want) {
		t.Errorf("push webhooks = %v, want %v", got, want)
	}
	var headSHA sql.NullString
	if err := e.db.QueryRow(`SELECT head_sha FROM pull_requests WHERE id = $1`, pullID).Scan(&headSHA); err != nil {
		t.Fatalf("read head_sha: %v", err)
	}
	if headSHA.String != src.Second.String() {
		t.Errorf("pull head_sha = %q, want %s", headSHA.String, src.Second)
	}
	var events, notifications int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM events WHERE repo_id = $1`, e.repoID).Scan(&events)
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = $1`, e.ownerID).Scan(&notifications)
	if events != 0 || notifications != 0 {
		t.Errorf("events = %d, notifications = %d; want none", events, notifications)
	}

	if err := e.svc.Mirror.Sync(ctx, m); err != nil {
		t.Fatalf("unchanged Sync: %v", err)
	}
	select {
	case p := <-pushes:
		t.Errorf("unchanged sync sent a push webhook: %v", p)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestMirrorService_SealToken_NoKey(t *testing.T) {
	svc := service.New(store.New(nil), &config.Config{})
	if _, err := svc.Mirror.SealToken("x"); !errors.Is(err, service.ErrMirrorNoSecretKey) {
		t.Errorf("SealToken err = %v, want ErrMirrorNoSecretKey", err)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func writeBlob(t *testing.T, st storer.EncodedObjectStorer, content []byte) plumbing.Hash {
	t.Helper()
	obj := st.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, _ := obj.Writer()
	_, _ = w.Write(content)
	_ = w.Close()
	h, err := st.SetEncodedObject(obj)
	if err != nil {
		t.Fatalf("write blob: %v", err)
	}
	return h
}

func writeTree(t *testing.T, st storer.EncodedObjectStorer, blob plumbing.Hash) plumbing.Hash {
	t.Helper()
	return encodeObject(t, st, &object.Tree{Entries: []object.TreeEntry{{Name: "f.txt", Mode: filemode.Regular, Hash: blob}}})
}

func writeFileCommit(t *testing.T, st storer.EncodedObjectStorer, blob plumbing.Hash, msg string, parents ...plumbing.Hash) plumbing.Hash {
	t.Helper()
	return writeCommitWithTree(t, st, writeTree(t, st, blob), msg, parents...)
}

func writeCommitWithTree(t *testing.T, st storer.EncodedObjectStorer, tree plumbing.Hash, msg string, parents ...plumbing.Hash) plumbing.Hash {
	t.Helper()
	sig := object.Signature{Name: "Tester", Email: "tester@example.com", When: time.Unix(0, 0).UTC()}
	return encodeObject(t, st, &object.Commit{Author: sig, Committer: sig, Message: msg, TreeHash: tree, ParentHashes: parents})
}

func encodeObject(t *testing.T, st storer.EncodedObjectStorer, o object.Object) plumbing.Hash {
	t.Helper()
	obj := st.NewEncodedObject()
	if err := o.Encode(obj); err != nil {
		t.Fatalf("encode: %v", err)
	}
	h, err := st.SetEncodedObject(obj)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return h
}
