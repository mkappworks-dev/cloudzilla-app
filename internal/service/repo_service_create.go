package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type RepoInitOptions struct {
	AddREADME bool
	Gitignore string // gitignore template name, "" = none
	License   string // license key, "" = none
}

func (o RepoInitOptions) any() bool {
	return o.AddREADME || o.Gitignore != "" || o.License != ""
}

// personalOwner takes the owner's ID as well as its name: a JWT outlives its
// account, and the name it carries may since have been registered by someone else.
func (s *RepoService) personalOwner(ctx context.Context, id int64, username string) (*model.User, error) {
	owner, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("owner not found: %w", err)
	}
	if owner.ID != id {
		return nil, fmt.Errorf("owner not found: %s is no longer user %d", username, id)
	}
	return owner, nil
}

// RepoTarget is the namespace a new repo lands in: OwnerID for a personal
// repo, OrgID for an org repo.
type RepoTarget struct {
	ActorID   int64
	OwnerName string
	OwnerID   int64
	OrgID     int64
}

var errTargetOwner = fmt.Errorf("you can create repositories only in your account or an organization you own: %w", ErrForbidden)

// ResolveRepoTarget maps ownerName to the actor's own account (also for "")
// or to an org the actor owns.
func (s *RepoService) ResolveRepoTarget(ctx context.Context, actorID int64, actorUsername, ownerName string) (RepoTarget, error) {
	if ownerName == "" || ownerName == actorUsername {
		if _, err := s.personalOwner(ctx, actorID, actorUsername); err != nil {
			return RepoTarget{}, err
		}
		return RepoTarget{ActorID: actorID, OwnerName: actorUsername, OwnerID: actorID}, nil
	}
	org, err := s.orgs.GetByName(ctx, ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return RepoTarget{}, errTargetOwner
	}
	if err != nil {
		return RepoTarget{}, err
	}
	if !s.isOrgOwner(ctx, org.ID, actorID) {
		return RepoTarget{}, errTargetOwner
	}
	return RepoTarget{ActorID: actorID, OwnerName: org.Name, OrgID: org.ID}, nil
}

func (s *RepoService) Create(ctx context.Context, ownerID int64, ownerUsername, name, description string, private bool, init RepoInitOptions) (*model.Repository, error) {
	if err := ValidateRepoName(name); err != nil {
		return nil, fmt.Errorf("invalid repository name: %w", err)
	}

	owner, err := s.personalOwner(ctx, ownerID, ownerUsername)
	if err != nil {
		return nil, err
	}

	if err := s.quota.CheckNewRepo(ctx, QuotaOwner{UserID: owner.ID}); err != nil {
		return nil, err
	}
	repoPath, err := claimRepo(ctx, s.repos, s.cfg.ReposRoot, ownerUsername, name)
	if err != nil {
		return nil, err
	}
	r := &model.Repository{
		OwnerID:       owner.ID,
		CreatedBy:     owner.ID,
		OwnerName:     ownerUsername,
		Name:          name,
		Description:   description,
		Private:       private,
		DefaultBranch: "main",
	}
	if err := s.repos.CreateWithOwnerName(ctx, r); err != nil {
		abandonNewRepo(ctx, s.repos, 0, repoPath)
		return nil, repoNameErr("create repo", err)
	}
	if err := initBareRepo(repoPath, r.DefaultBranch); err != nil {
		abandonNewRepo(ctx, s.repos, r.ID, repoPath)
		return nil, fmt.Errorf("git init bare: %w", err)
	}

	if init.any() {
		// The DB row and bare repo already exist. A failure here leaves a valid
		// empty repo the user can still push to, so we log and return success
		// rather than 500-ing on already-created state.
		sig := commitAuthorFor(s.noreplyHost, owner).signature(time.Now().UTC())
		if err := seedInitialCommit(repoPath, r.DefaultBranch, sig, init, owner.Username, name, description); err != nil {
			slog.Error("seed initial commit for new repo failed; repo created empty",
				"repo_id", r.ID, "owner", ownerUsername, "name", name, "error", err)
		}
	}

	s.quota.Recompute(r)
	return r, nil
}

// initBareRepo creates an empty bare repo whose HEAD names defaultBranch;
// go-git would otherwise point it at master, which readers of HEAD then miss.
func initBareRepo(path, defaultBranch string) error {
	_, err := gogit.PlainInitWithOptions(path, &gogit.PlainInitOptions{
		Bare:        true,
		InitOptions: gogit.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName(defaultBranch)},
	})
	return err
}

// seedInitialCommit writes the objects into the bare repo itself: a go-git push
// to a local path execs git-receive-pack, and the Docker image ships no git.
func seedInitialCommit(bareDir, defaultBranch string, sig object.Signature, init RepoInitOptions, ownerName, repoName, description string) error {
	files := map[string]string{}

	if init.AddREADME {
		readme := "# " + repoName + "\n"
		if d := strings.TrimSpace(description); d != "" {
			readme += "\n" + d + "\n"
		}
		files["README.md"] = readme
	}
	if init.Gitignore != "" {
		if content, ok := gitignoreContent(init.Gitignore); ok {
			files[".gitignore"] = content
		} else {
			return fmt.Errorf("unknown gitignore template %q", init.Gitignore)
		}
	}
	if init.License != "" {
		if content, ok := licenseContent(init.License, ownerName); ok {
			files["LICENSE"] = content
		} else {
			return fmt.Errorf("unknown license %q", init.License)
		}
	}
	if len(files) == 0 {
		return nil
	}

	bare, err := gogit.PlainOpen(bareDir)
	if err != nil {
		return fmt.Errorf("open bare: %w", err)
	}

	entries := make([]object.TreeEntry, 0, len(files))
	for name, content := range files {
		blob, err := writeBlob(bare, []byte(content))
		if err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Regular, Hash: blob})
	}
	tree, err := writeTree(bare, entries)
	if err != nil {
		return fmt.Errorf("write tree: %w", err)
	}

	commit := &object.Commit{Author: sig, Committer: sig, Message: "Initial commit", TreeHash: tree}
	commitObj := bare.Storer.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		return fmt.Errorf("encode commit: %w", err)
	}
	commitHash, err := bare.Storer.SetEncodedObject(commitObj)
	if err != nil {
		return fmt.Errorf("write commit: %w", err)
	}

	branch := defaultBranch
	if branch == "" {
		branch = "main"
	}
	branchRef := plumbing.NewBranchReferenceName(branch)
	if err := gitref.Move(bare.Storer, branchRef, plumbing.ZeroHash, commitHash); err != nil {
		return fmt.Errorf("create %s: %w", branchRef, err)
	}
	if err := bare.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branchRef)); err != nil {
		return fmt.Errorf("set bare HEAD: %w", err)
	}

	return nil
}

var (
	// ErrTemplateNotFound also covers private repos, so it never confirms one exists.
	ErrTemplateNotFound = errors.New("template repo not found")
	ErrNotTemplate      = errors.New("repository is not a template")
	ErrTemplateArchived = errors.New("template repo is archived")
)

func (s *RepoService) CreateFromTemplate(ctx context.Context, templateRepoID, newOwnerID int64, newOwnerUsername, newName, description string) (*model.Repository, error) {
	if err := ValidateRepoName(newName); err != nil {
		return nil, fmt.Errorf("invalid repository name: %w", err)
	}
	tmpl, err := s.repos.GetByID(ctx, templateRepoID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && tmpl.Private) {
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get template repo: %w", err)
	}
	if !tmpl.IsTemplate {
		return nil, ErrNotTemplate
	}
	if tmpl.IsArchived {
		return nil, ErrTemplateArchived
	}
	if _, err := s.personalOwner(ctx, newOwnerID, newOwnerUsername); err != nil {
		return nil, err
	}
	if err := s.quota.CheckNewRepo(ctx, QuotaOwner{UserID: newOwnerID}); err != nil {
		return nil, err
	}

	dstPath, err := claimRepo(ctx, s.repos, s.cfg.ReposRoot, newOwnerUsername, newName)
	if err != nil {
		return nil, err
	}
	newRepo := &model.Repository{
		OwnerID:       newOwnerID,
		CreatedBy:     newOwnerID,
		OwnerName:     newOwnerUsername,
		Name:          newName,
		Description:   description,
		Private:       false,
		DefaultBranch: tmpl.DefaultBranch,
	}
	if err := s.repos.CreateWithOwnerName(ctx, newRepo); err != nil {
		abandonNewRepo(ctx, s.repos, 0, dstPath)
		return nil, repoNameErr("create repo from template", err)
	}

	srcPath, _ := repoDirs(s.cfg.ReposRoot, tmpl.OwnerName, tmpl.Name)
	if _, statErr := os.Stat(srcPath); statErr == nil {
		if err := copyDir(srcPath, dstPath); err != nil {
			abandonNewRepo(ctx, s.repos, newRepo.ID, dstPath)
			return nil, fmt.Errorf("copy template git dir: %w", err)
		}
	} else if err := initBareRepo(dstPath, newRepo.DefaultBranch); err != nil {
		abandonNewRepo(ctx, s.repos, newRepo.ID, dstPath)
		return nil, fmt.Errorf("git init bare for template copy: %w", err)
	}

	s.quota.Recompute(newRepo)
	return newRepo, nil
}

func (s *RepoService) ListTemplates(ctx context.Context) ([]model.Repository, error) {
	return s.repos.ListTemplates(ctx)
}
