package handler_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
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

// receivePack pushes cmds over smart HTTP as the repo owner, with a pack of the
// commits they point at, and returns the per-ref report status.
func receivePack(t *testing.T, h http.Handler, r raceRepo, cmds ...*packp.Command) map[plumbing.ReferenceName]string {
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
	_ = upd.Capabilities.Set(capability.ReportStatus)
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
	if rr.Code != http.StatusOK {
		t.Fatalf("receive-pack: %d %q", rr.Code, rr.Body.String())
	}

	status := packp.NewReportStatus()
	if err := status.Decode(rr.Body); err != nil {
		t.Fatalf("decode report status: %v", err)
	}
	refs := make(map[plumbing.ReferenceName]string)
	for _, cs := range status.CommandStatuses {
		refs[cs.ReferenceName] = cs.Status
	}
	return refs
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
	if got := branchHash(t, r.git, "main"); got != web {
		t.Errorf("main = %s, want web commit %s", got, web)
	}
}

// A refused ref in the same push must not skip branch protection for the refs
// that were applied.
func TestGitReceivePack_RefusedRefStillEnforcesProtection(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	h := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	testutil.Exec(t, db, `INSERT INTO branch_protections (repo_id, pattern, block_force_push) VALUES ($1, 'main', true)`, r.id)

	refs := receivePack(t, h, r,
		&packp.Command{Name: mainRef, Old: r.mainTip, New: r.featurePushed},
		&packp.Command{Name: featureRef, Old: plumbing.ZeroHash, New: r.featurePushed},
	)

	if got := refs[featureRef]; got == "ok" {
		t.Errorf("feature status = ok, want refused: feature already exists")
	}
	if got := branchHash(t, r.git, "main"); got != r.mainTip {
		t.Errorf("main = %s, want force push rolled back to %s", got, r.mainTip)
	}
}
