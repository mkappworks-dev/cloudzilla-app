package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ErrRepoNameTaken also covers a directory on disk with no row, so a create
// cannot tell the two apart.
var ErrRepoNameTaken = errors.New("a repository with that name already exists")

// ErrOwnsOrgRepos: an org repo's owner_id is the member who created it, so
// deleting that member's account would cascade the repo out of the org.
var ErrOwnsOrgRepos = errors.New("delete the organization repositories you created first")

// ErrRepoNameReserved: repo <x>.wiki's git dir would be repo <x>'s wiki dir.
var ErrRepoNameReserved = errors.New("names ending in .wiki are reserved for wikis")

const wikiSuffix = ".wiki"

// Case-insensitive because the filesystem may be.
func isWikiName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), wikiSuffix)
}

// ValidateRepoName is ValidateName plus the names a new repo may not take.
// Git access keeps using ValidateName: repos created before the reservation
// still need to be reachable.
func ValidateRepoName(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if isWikiName(name) {
		return ErrRepoNameReserved
	}
	return nil
}

// wikiPartner is the name whose dirs overlap name's: <x> and <x>.wiki.
func wikiPartner(name string) string {
	if isWikiName(name) {
		return name[:len(name)-len(wikiSuffix)]
	}
	return name + wikiSuffix
}

// Every lifecycle step moves both dirs together: a wiki left behind is served
// to the next repo that takes the name.
func repoDirs(root, owner, name string) (gitDir, wikiDir string) {
	base := filepath.Join(root, owner, name)
	return base + ".git", base + ".wiki.git"
}

// ownWikiDir is "" when a repo named <name>.wiki, possible only from before
// the reservation, holds owner/name's wiki path as its git dir (or will on
// restore): owner/name must then neither serve nor move that path.
func (s *RepoService) ownWikiDir(ctx context.Context, owner, name string) (string, error) {
	held, err := s.repos.NameHeld(ctx, owner, name+wikiSuffix)
	if err != nil || held {
		return "", err
	}
	_, wikiDir := repoDirs(s.cfg.ReposRoot, owner, name)
	return wikiDir, nil
}

// WikiEnabled fails closed when the wiki path cannot be checked.
func (s *RepoService) WikiEnabled(ctx context.Context, repo *model.Repository) bool {
	if !repo.AllowWiki {
		return false
	}
	wikiDir, err := s.ownWikiDir(ctx, repo.OwnerName, repo.Name)
	if err != nil {
		slog.Error("check wiki path", "repo_id", repo.ID, "error", err)
		return false
	}
	return wikiDir != ""
}

// A path that cannot be checked counts as taken.
func pathTaken(path string) bool {
	_, err := os.Lstat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// claimRepo reserves owner/name for a new row. os.Mkdir fails on an existing
// path, so the row never adopts data an earlier holder of the name left on
// disk; the wiki path is checked too because wikis are created lazily.
func claimRepo(ctx context.Context, repos *store.RepoStore, root, owner, name string) (string, error) {
	if isWikiName(name) {
		return "", ErrRepoNameReserved
	}
	if _, err := repos.GetByOwnerName(ctx, owner, name); err == nil {
		return "", ErrRepoNameTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if held, err := repos.NameHeld(ctx, owner, wikiPartner(name)); err != nil {
		return "", err
	} else if held {
		return "", ErrRepoNameTaken
	}
	gitDir, wikiDir := repoDirs(root, owner, name)
	if err := os.MkdirAll(filepath.Dir(gitDir), 0o755); err != nil {
		return "", fmt.Errorf("create owner dir: %w", err)
	}
	if pathTaken(wikiDir) {
		return "", ErrRepoNameTaken
	}
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", ErrRepoNameTaken
		}
		return "", fmt.Errorf("create repo dir: %w", err)
	}
	return gitDir, nil
}

// Handlers show ErrRepoNameTaken's message, so it is returned unwrapped.
func repoNameErr(op string, err error) error {
	if errors.Is(err, store.ErrRepoNameInUse) {
		return ErrRepoNameTaken
	}
	return fmt.Errorf("%s: %w", op, err)
}

// abandonNewRepo undoes a create that failed after claimRepo. It ignores the
// request's cancellation: a row left behind would outlive its directory.
func abandonNewRepo(ctx context.Context, repos *store.RepoStore, repoID int64, gitDir string) {
	if repoID != 0 {
		if err := repos.DeleteByID(context.WithoutCancel(ctx), repoID); err != nil {
			slog.Error("delete row of failed repo create", "repo_id", repoID, "error", err)
		}
	}
	if err := os.RemoveAll(gitDir); err != nil {
		slog.Error("remove dir of failed repo create", "path", gitDir, "error", err)
	}
}

func deletedSuffix(now time.Time) string {
	return ".deleted." + strconv.FormatInt(now.Unix(), 10)
}

type dirMove struct{ from, to string }

func movesAside(suffix string, dirs ...string) []dirMove {
	moves := make([]dirMove, len(dirs))
	for i, dir := range dirs {
		moves[i] = dirMove{from: dir, to: dir + suffix}
	}
	return moves
}

// renameDirs applies the moves whose source exists, all or nothing. os.Rename
// refuses to replace an existing directory, so no move lands on another repo.
func renameDirs(moves []dirMove) ([]dirMove, error) {
	var done []dirMove
	for _, m := range moves {
		if err := os.Rename(m.from, m.to); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			revertDirs(done)
			return nil, fmt.Errorf("move %s: %w", filepath.Base(m.from), err)
		}
		done = append(done, m)
	}
	return done, nil
}

// A move that cannot be reverted leaves the dir at its aside path, where
// nothing serves it.
func revertDirs(done []dirMove) {
	for i := len(done) - 1; i >= 0; i-- {
		if err := os.Rename(done[i].to, done[i].from); err != nil {
			slog.Error("revert repo dir move failed", "from", done[i].to, "to", done[i].from, "error", err)
		}
	}
}

// deletedCopySuffix finds the copy r's soft delete moved aside, never another
// holder's copy of the same name. Rows deleted before deleted_at took the
// rename's clock were stamped by the DB just after it, up to a second later.
func deletedCopySuffix(root string, r model.Repository) (string, bool) {
	if r.DeletedAt == nil {
		return "", false
	}
	gitDir, wikiDir := repoDirs(root, r.OwnerName, r.Name)
	for _, at := range []time.Time{*r.DeletedAt, r.DeletedAt.Add(-time.Second)} {
		suffix := deletedSuffix(at)
		if pathTaken(gitDir+suffix) || pathTaken(wikiDir+suffix) {
			return suffix, true
		}
	}
	return "", false
}

func removeDeletedCopy(root string, r model.Repository) {
	suffix, ok := deletedCopySuffix(root, r)
	if !ok {
		return
	}
	gitDir, wikiDir := repoDirs(root, r.OwnerName, r.Name)
	removeDirs(gitDir+suffix, wikiDir+suffix)
}

// removeStrandedWiki removes the wiki a soft delete from before wikis moved
// with their repo left at the live path, once no row holds the name: nothing
// can restore it then, and it would refuse every new claim on the name.
func (s *RepoService) removeStrandedWiki(ctx context.Context, owner, name string) {
	wikiDir, err := s.ownWikiDir(ctx, owner, name)
	if err != nil {
		slog.Warn("check stranded wiki", "owner", owner, "name", name, "error", err)
		return
	}
	if wikiDir == "" || !pathTaken(wikiDir) {
		return
	}
	held, err := s.repos.NameHeld(ctx, owner, name)
	if err != nil {
		slog.Warn("check stranded wiki", "owner", owner, "name", name, "error", err)
		return
	}
	if !held {
		removeDirs(wikiDir)
	}
}

func removeDirs(dirs ...string) {
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			slog.Warn("remove repo dir failed", "path", dir, "error", err)
		}
	}
}

// DeleteWithOwner runs deleteOwner, whose row delete cascades to every repo
// ownerID owns, with the owner's personal repo dirs moved aside first. A
// failed delete puts them back; a successful one removes them and the copies
// of the owner's soft-deleted repos, which no row is left to restore or purge.
func (s *RepoService) DeleteWithOwner(ctx context.Context, ownerID int64, deleteOwner func() error) error {
	repos, err := s.repos.ListAllByOwnerID(ctx, ownerID)
	if err != nil {
		return err
	}
	var softDeleted []model.Repository
	var dirs []string
	for _, r := range repos {
		switch {
		case r.DeletedAt != nil:
			softDeleted = append(softDeleted, r)
		case r.OrgID != 0:
			return ErrOwnsOrgRepos
		default:
			gitDir, wikiDir := repoDirs(s.cfg.ReposRoot, r.OwnerName, r.Name)
			dirs = append(dirs, gitDir, wikiDir)
		}
	}

	moved, err := renameDirs(movesAside(deletedSuffix(time.Now()), dirs...))
	if err != nil {
		return fmt.Errorf("move repos aside: %w", err)
	}
	if err := deleteOwner(); err != nil {
		revertDirs(moved)
		return err
	}
	for _, m := range moved {
		removeDirs(m.to)
	}
	for _, r := range softDeleted {
		removeDeletedCopy(s.cfg.ReposRoot, r)
		s.removeStrandedWiki(ctx, r.OwnerName, r.Name)
	}
	return nil
}
