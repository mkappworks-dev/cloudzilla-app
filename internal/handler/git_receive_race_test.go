package handler_test

import (
	"bytes"
	"database/sql"
	"io"
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

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var (
	mainRef    = plumbing.NewBranchReferenceName("main")
	featureRef = plumbing.NewBranchReferenceName("feature")
	topicRef   = plumbing.NewBranchReferenceName("topic")
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
// the commits they point at.
func postReceivePack(t *testing.T, h http.Handler, r raceRepo, reportStatus bool, cmds ...*packp.Command) *httptest.ResponseRecorder {
	t.Helper()
	var tips []plumbing.Hash
	for _, cmd := range cmds {
		if !cmd.New.IsZero() {
			tips = append(tips, cmd.New)
		}
	}
	var pack bytes.Buffer
	if _, err := packfile.NewEncoder(&pack, r.git.Storer, false).Encode(tips, 0); err != nil {
		t.Fatalf("encode pack: %v", err)
	}
	upd := packp.NewReferenceUpdateRequest()
	if reportStatus {
		_ = upd.Capabilities.Set(capability.ReportStatus)
	}
	upd.Commands = cmds
	upd.Packfile = io.NopCloser(&pack)
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
// ref. The status must be the whole response body.
func receivePack(t *testing.T, h http.Handler, r raceRepo, cmds ...*packp.Command) map[plumbing.ReferenceName]string {
	t.Helper()
	rr := postReceivePack(t, h, r, true, cmds...)
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

func assertRef(t *testing.T, r raceRepo, branch string, want plumbing.Hash) {
	t.Helper()
	if got := branchHash(t, r.git, branch); got != want {
		t.Errorf("%s = %s, want %s", branch, got, want)
	}
}

func TestGitReceivePack_BranchMovedSinceClientRead_Refused(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(db, reposRoot)
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
	h := newAPIRouterAt(db, reposRoot)
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
	h := newAPIRouterAt(db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	protectMain(t, db, r)

	refs := receivePack(t, h, r, &packp.Command{Name: mainRef, Old: r.mainTip, New: r.mainPushed})

	if got := refs[mainRef]; got != "ok" {
		t.Errorf("main status = %q, want ok", got)
	}
	assertRef(t, r, "main", r.mainPushed)
}

func TestGitReceivePack_ProtectedAndUnprotectedRefs_AppliesOnlyUnprotected(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(db, reposRoot)
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
	h := newAPIRouterAt(db, reposRoot)
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
	h := newAPIRouterAt(db, reposRoot)
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
