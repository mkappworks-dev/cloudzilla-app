package handler_test

import (
	"bytes"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var (
	mainRef    = plumbing.NewBranchReferenceName("main")
	featureRef = plumbing.NewBranchReferenceName("feature")
	topicRef   = plumbing.NewBranchReferenceName("topic")

	missingCommit = plumbing.NewHash("1234567890123456789012345678901234567890")
)

// webCommit commits a file on branch through the web and returns the new tip.
func webCommit(t *testing.T, reposRoot string, r raceRepo, branch string) plumbing.Hash {
	t.Helper()
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	if err := code.CommitFile(r.owner.name, r.name, branch, "web.txt", []byte("web\n"), raceAuthor, "Edit on the web"); err != nil {
		t.Fatalf("web commit on %s: %v", branch, err)
	}
	return branchHash(t, r.git, branch)
}

func protectMain(t *testing.T, db *sql.DB, r raceRepo) {
	t.Helper()
	testutil.Exec(t, db, `INSERT INTO branch_protections (repo_id, pattern, block_force_push) VALUES ($1, 'main', true)`, r.id)
}

// postReceivePack pushes cmds over smart HTTP as the repo owner, with a pack of
// the commits they point at. A commit r lacks is left out of the pack, as a
// client that omits it would.
func postReceivePack(t *testing.T, h http.Handler, r raceRepo, reportStatus bool, cmds ...*packp.Command) *httptest.ResponseRecorder {
	t.Helper()
	var tips []plumbing.Hash
	for _, cmd := range cmds {
		if r.git.Storer.HasEncodedObject(cmd.New) == nil {
			tips = append(tips, cmd.New)
		}
	}
	var pack bytes.Buffer
	if _, err := packfile.NewEncoder(&pack, r.git.Storer, false).Encode(tips, 0); err != nil {
		t.Fatalf("encode pack: %v", err)
	}
	return postPack(t, h, r, reportStatus, pack.Bytes(), cmds...)
}

// postPack pushes cmds and pack over smart HTTP as the repo owner.
func postPack(t *testing.T, h http.Handler, r raceRepo, reportStatus bool, pack []byte, cmds ...*packp.Command) *httptest.ResponseRecorder {
	t.Helper()
	upd := packp.NewReferenceUpdateRequest()
	if reportStatus {
		_ = upd.Capabilities.Set(capability.ReportStatus)
	}
	upd.Commands = cmds
	upd.Packfile = io.NopCloser(bytes.NewReader(pack))
	var body bytes.Buffer
	if err := upd.Encode(&body); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, r.path+"/git-receive-pack", &body)
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	req.Header.Set("Authorization", "Bearer "+r.owner.token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// receivePack pushes cmds with report-status and returns the status of each
// ref.
func receivePack(t *testing.T, h http.Handler, r raceRepo, cmds ...*packp.Command) map[plumbing.ReferenceName]string {
	t.Helper()
	return reportedRefs(t, postReceivePack(t, h, r, true, cmds...))
}

// receivePackOf is receivePack with pack as the pushed objects.
func receivePackOf(t *testing.T, h http.Handler, r raceRepo, pack []byte, cmds ...*packp.Command) map[plumbing.ReferenceName]string {
	t.Helper()
	return reportedRefs(t, postPack(t, h, r, true, pack, cmds...))
}

// reportedRefs is the status of each ref in rr, whose body must be only a
// report status.
func reportedRefs(t *testing.T, rr *httptest.ResponseRecorder) map[plumbing.ReferenceName]string {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("receive-pack: %d %q", rr.Code, rr.Body.String())
	}

	status := packp.NewReportStatus()
	if err := status.Decode(rr.Body); err != nil {
		t.Fatalf("decode report status: %v", err)
	}
	if rr.Body.Len() > 0 {
		t.Errorf("response continues after the report status: %q", rr.Body.String())
	}
	refs := make(map[plumbing.ReferenceName]string)
	for _, cs := range status.CommandStatuses {
		refs[cs.ReferenceName] = cs.Status
	}
	return refs
}

// treeOf returns the tree of commit: an object r has that isn't a commit.
func treeOf(t *testing.T, r raceRepo, commit plumbing.Hash) plumbing.Hash {
	t.Helper()
	c, err := r.git.CommitObject(commit)
	if err != nil {
		t.Fatalf("read %s: %v", commit, err)
	}
	return c.TreeHash
}

func assertRef(t *testing.T, r raceRepo, branch string, want plumbing.Hash) {
	t.Helper()
	if got := branchHash(t, r.git, branch); got != want {
		t.Errorf("%s = %s, want %s", branch, got, want)
	}
}

func TestGitReceivePack_BranchMovedSinceClientRead_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	web := webCommit(t, reposRoot, r, "main")

	refs := receivePack(t, h, r, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	if got := refs[mainRef]; !strings.HasPrefix(got, service.ErrRefMoved.Error()) {
		t.Errorf("main status = %q, want %q", got, service.ErrRefMoved.Error())
	}
	assertRef(t, r, "main", web)
}

func TestGitReceivePack_ForcePushToProtectedBranch_RefusedWithoutWrite(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	protectMain(t, db, r)
	// Any write to main now fails with a permission error instead of landing,
	// so a push that moves main even briefly can't report the protection reason.
	if err := os.Chmod(filepath.Join(r.gitDir, mainRef.String()), 0o444); err != nil {
		t.Fatalf("make main read-only: %v", err)
	}

	refs := receivePack(t, h, r, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.featurePushed})

	if got := refs[mainRef]; got != service.ErrForcePushBlocked.Error() {
		t.Errorf("main status = %q, want %q", got, service.ErrForcePushBlocked.Error())
	}
	assertRef(t, r, "main", r.mainTip)
}

func TestGitReceivePack_FastForwardToProtectedBranch_Applies(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	protectMain(t, db, r)

	refs := receivePack(t, h, r, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	if got := refs[mainRef]; got != "ok" {
		t.Errorf("main status = %q, want ok", got)
	}
	assertRef(t, r, "main", r.mainPushed)
}

// Like git: a branch is read as a commit everywhere, protected or not.
func TestGitReceivePack_BranchToNonCommit_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	protectMain(t, db, r)
	tree := treeOf(t, r, r.mainPushed)

	refs := receivePack(t, h, r,
		&packp.Command{Name: mainRef, Old: r.mainTip, New: tree},
		&packp.Command{Name: featureRef, Old: r.featureTip, New: tree},
		&packp.Command{Name: topicRef, Old: plumbing.ZeroHash, New: tree},
	)

	for _, ref := range []plumbing.ReferenceName{mainRef, featureRef, topicRef} {
		if got := refs[ref]; got != gittransport.ErrNonCommitBranch.Error() {
			t.Errorf("%s status = %q, want %q", ref, got, gittransport.ErrNonCommitBranch.Error())
		}
	}
	assertRef(t, r, "main", r.mainTip)
	assertRef(t, r, "feature", r.featureTip)
	if _, err := r.git.Reference(topicRef, false); err != plumbing.ErrReferenceNotFound {
		t.Errorf("topic lookup err = %v, want %v", err, plumbing.ErrReferenceNotFound)
	}
}

// go-git's receive-pack doesn't check that a pushed object exists, so any ref
// could be left pointing at nothing.
func TestGitReceivePack_UnprotectedBranchToMissingCommit_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)

	refs := receivePack(t, h, r, &packp.Command{Name: featureRef, Old: r.featureTip, New: missingCommit})

	if got := refs[featureRef]; got != gittransport.ErrMissingObjects.Error() {
		t.Errorf("feature status = %q, want %q", got, gittransport.ErrMissingObjects.Error())
	}
	assertRef(t, r, "feature", r.featureTip)
}

func TestGitReceivePack_CreateTagAtMissingCommit_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	tagRef := plumbing.NewTagReferenceName("v1")

	refs := receivePack(t, h, r, &packp.Command{Name: tagRef, Old: plumbing.ZeroHash, New: missingCommit})

	if got := refs[tagRef]; got != gittransport.ErrMissingObjects.Error() {
		t.Errorf("v1 status = %q, want %q", got, gittransport.ErrMissingObjects.Error())
	}
	if _, err := r.git.Reference(tagRef, false); err != plumbing.ErrReferenceNotFound {
		t.Errorf("v1 lookup err = %v, want %v", err, plumbing.ErrReferenceNotFound)
	}
}

// A push must carry all the history it adds, not just its tip.
func TestGitReceivePack_CommitWithMissingParent_RefusedOthersApply(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	client := memory.NewStorage()
	orphan := testutil.WriteCommit(t, client, "orphan", missingCommit)

	refs := receivePackOf(t, h, r, testutil.PackAll(t, client),
		&packp.Command{Name: featureRef, Old: r.featureTip, New: orphan},
		&packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed},
	)

	if got := refs[featureRef]; got != gittransport.ErrMissingObjects.Error() {
		t.Errorf("feature status = %q, want %q", got, gittransport.ErrMissingObjects.Error())
	}
	if got := refs[mainRef]; got != "ok" {
		t.Errorf("main status = %q, want ok", got)
	}
	assertRef(t, r, "feature", r.featureTip)
	assertRef(t, r, "main", r.mainPushed)
}

// go-git stores a pack before it updates any ref, so a refused push leaves its
// objects in the repo; a later push can't build on them.
func TestGitReceivePack_OntoRefusedPushObjects_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	first := memory.NewStorage()
	orphan := testutil.WriteCommit(t, first, "orphan", missingCommit)
	refs := receivePackOf(t, h, r, testutil.PackAll(t, first), &packp.Command{Name: featureRef, Old: r.featureTip, New: orphan})
	if got := refs[featureRef]; got != gittransport.ErrMissingObjects.Error() {
		t.Fatalf("first push: feature status = %q, want %q", got, gittransport.ErrMissingObjects.Error())
	}
	next := memory.NewStorage()
	tip := testutil.WriteCommit(t, next, "tip", orphan)

	refs = receivePackOf(t, h, r, testutil.PackAll(t, next), &packp.Command{Name: featureRef, Old: r.featureTip, New: tip})

	if got := refs[featureRef]; got != gittransport.ErrMissingObjects.Error() {
		t.Errorf("feature status = %q, want %q", got, gittransport.ErrMissingObjects.Error())
	}
	assertRef(t, r, "feature", r.featureTip)
}

// go-git sends a vet error's text to the pusher, so a failed rule lookup is
// logged, not reported.
func TestGitReceivePack_ProtectionLookupFails_RefusedWithoutDBError(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `ALTER TABLE branch_protections RENAME TO branch_protections_gone`)
	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })

	refs := receivePack(t, h, r, &packp.Command{Name: featureRef, Old: r.featureTip, New: r.featurePushed})

	if got := refs[featureRef]; got != service.ErrProtectionCheckFailed.Error() {
		t.Errorf("feature status = %q, want %q", got, service.ErrProtectionCheckFailed.Error())
	}
	assertRef(t, r, "feature", r.featureTip)
	if !strings.Contains(logs.String(), "SQLSTATE 42P01") {
		t.Errorf("rule lookup error not logged:\n%s", logs.String())
	}
}

// Deleting a branch and pushing it again is a force push in two steps.
func TestGitReceivePack_DeleteProtectedBranch_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	protectMain(t, db, r)

	refs := receivePack(t, h, r, &packp.Command{Name: mainRef, Old: r.mainTip, New: plumbing.ZeroHash})

	if got := refs[mainRef]; got != service.ErrForcePushBlocked.Error() {
		t.Errorf("main status = %q, want %q", got, service.ErrForcePushBlocked.Error())
	}
	assertRef(t, r, "main", r.mainTip)
}

func TestGitReceivePack_ProtectedAndUnprotectedRefs_AppliesOnlyUnprotected(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	protectMain(t, db, r)

	refs := receivePack(t, h, r,
		&packp.Command{Name: mainRef, Old: r.mainTip, New: r.featurePushed},
		&packp.Command{Name: featureRef, Old: r.featureTip, New: r.featurePushed},
	)

	if got := refs[mainRef]; got != service.ErrForcePushBlocked.Error() {
		t.Errorf("main status = %q, want %q", got, service.ErrForcePushBlocked.Error())
	}
	if got := refs[featureRef]; got != "ok" {
		t.Errorf("feature status = %q, want ok", got)
	}
	assertRef(t, r, "main", r.mainTip)
	assertRef(t, r, "feature", r.featurePushed)
}

// A refused ref in the same push must not skip branch protection for the rest.
func TestGitReceivePack_RefusedRefStillEnforcesProtection(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	protectMain(t, db, r)

	refs := receivePack(t, h, r,
		&packp.Command{Name: mainRef, Old: r.mainTip, New: r.featurePushed},
		&packp.Command{Name: featureRef, Old: plumbing.ZeroHash, New: r.featurePushed},
	)

	if got := refs[featureRef]; got == "ok" {
		t.Errorf("feature status = ok, want refused: feature already exists")
	}
	if got := refs[mainRef]; got != service.ErrForcePushBlocked.Error() {
		t.Errorf("main status = %q, want %q", got, service.ErrForcePushBlocked.Error())
	}
	assertRef(t, r, "main", r.mainTip)
}

// A client that doesn't ask for report-status gets no per-ref result, but its
// push is judged ref by ref all the same.
func TestGitReceivePack_NoReportStatus_RefusesPerRef(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	protectMain(t, db, r)

	rr := postReceivePack(t, h, r, false,
		&packp.Command{Name: mainRef, Old: r.mainTip, New: r.featurePushed},
		&packp.Command{Name: featureRef, Old: plumbing.ZeroHash, New: r.featurePushed},
		&packp.Command{Name: topicRef, Old: plumbing.ZeroHash, New: r.featurePushed},
	)

	if rr.Code != http.StatusOK || rr.Body.Len() != 0 {
		t.Errorf("want 200 with an empty body, got %d %q", rr.Code, rr.Body.String())
	}
	assertRef(t, r, "main", r.mainTip)
	assertRef(t, r, "feature", r.featureTip)
	assertRef(t, r, "topic", r.featurePushed)
}
