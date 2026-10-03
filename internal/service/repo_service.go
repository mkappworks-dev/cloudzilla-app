package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

var ErrInvalidRepoName = errors.New("invalid repository name")

// ValidateName checks that a repository or owner name is safe for filesystem
// use and URL routing. Names must start with an alphanumeric character and
// contain only alphanumeric, dot, underscore, or hyphen characters.
func ValidateName(name string) error {
	if len(name) == 0 || len(name) > 100 {
		return fmt.Errorf("%w: must be 1-100 characters", ErrInvalidRepoName)
	}
	if !validNameRe.MatchString(name) {
		return fmt.Errorf("%w: contains invalid characters", ErrInvalidRepoName)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: reserved", ErrInvalidRepoName)
	}
	return nil
}

// RepoService manages repository creation, access control, and git directory lifecycle.
type RepoService struct {
	repos            *store.RepoStore
	users            *store.UserStore
	noreplyHost      string
	orgs             *store.OrgStore
	contributorStats *ContributorStatsService
	code             *CodeService
	language         *LanguageService
	pulls            *store.PullStore
	transfers        *store.RepoTransferStore
	cfg              config.GitConfig
}

// The code service may be nil in tests that do not exercise contributor queries.
func NewRepoService(repos *store.RepoStore, users *store.UserStore, orgs *store.OrgStore, contributorStats *ContributorStatsService, code *CodeService, cfg config.GitConfig) *RepoService {
	return &RepoService{repos: repos, users: users, orgs: orgs, contributorStats: contributorStats, code: code, cfg: cfg, noreplyHost: defaultNoreplyHost}
}

func (s *RepoService) WithNoreplyHostFrom(baseURL string) *RepoService {
	s.noreplyHost = noreplyHostFromBaseURL(baseURL)
	return s
}

func (s *RepoService) WithLanguageService(lang *LanguageService) *RepoService {
	s.language = lang
	return s
}

func (s *RepoService) WithPullStore(pulls *store.PullStore) *RepoService {
	s.pulls = pulls
	return s
}

func (s *RepoService) TopContributors(ctx context.Context, owner, name, ref string, limit int) ([]ContributorStat, error) {
	if s.code == nil {
		return nil, nil
	}
	all, err := s.code.GetContributors(owner, name, ref)
	if err != nil {
		return nil, err
	}
	if all, err = s.mergeContributorsByUser(ctx, all); err != nil {
		return nil, err
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// Git groups contributors by author email, so a user's pushed commits and their
// noreply-authored web commits arrive as separate entries. Merged entries take the
// username as Name because the sidebar links each avatar to "/"+Name.
func (s *RepoService) mergeContributorsByUser(ctx context.Context, stats []ContributorStat) ([]ContributorStat, error) {
	merged := make([]ContributorStat, 0, len(stats))
	indexByUser := make(map[int64]int)
	for _, c := range stats {
		u, err := userByAuthorEmail(ctx, s.users, c.Email)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if u == nil {
			merged = append(merged, c)
			continue
		}
		if i, ok := indexByUser[u.ID]; ok {
			merged[i].Commits += c.Commits
			merged[i].Additions += c.Additions
			merged[i].Deletions += c.Deletions
			continue
		}
		c.Name = u.Username
		indexByUser[u.ID] = len(merged)
		merged = append(merged, c)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Commits > merged[j].Commits })
	return merged, nil
}

type postReceiveCommit struct {
	AuthorEmail string
	AuthorTime  time.Time
	SHA         string
}

// Dedupes commits shared across multiple updated branches; returns an aggregate error only when every walk fails so a stuck repo doesn't go silent.
func (s *RepoService) OnPostReceive(ctx context.Context, repo *model.Repository, gitRepo *gogit.Repository, commands []*packp.Command) error {
	if s.contributorStats == nil || gitRepo == nil || repo == nil {
		return nil
	}
	seen := make(map[plumbing.Hash]struct{})
	var commits []postReceiveCommit
	var walkAttempts, walkFailures int
	for _, cmd := range commands {
		if cmd == nil {
			continue
		}
		if !strings.HasPrefix(cmd.Name.String(), "refs/heads/") {
			continue
		}
		if cmd.Action() == packp.Delete {
			continue
		}
		walkAttempts++
		walkErr := forEachPushedCommit(gitRepo, cmd, func(c *object.Commit) error {
			if _, dup := seen[c.Hash]; dup {
				return nil
			}
			seen[c.Hash] = struct{}{}
			commits = append(commits, postReceiveCommit{
				AuthorEmail: c.Author.Email,
				AuthorTime:  c.Author.When,
				SHA:         c.Hash.String(),
			})
			return nil
		})
		if walkErr != nil {
			walkFailures++
			slog.Warn("post-receive: commit walk failed",
				"repo_id", repo.ID, "ref", cmd.Name.String(), "new", cmd.New.String(), "error", walkErr)
		}
	}
	if walkAttempts > 0 && walkFailures == walkAttempts {
		return fmt.Errorf("commit stats: all %d branch walks failed (repo_id=%d)", walkAttempts, repo.ID)
	}
	if len(commits) == 0 {
		return nil
	}

	if s.contributorStats != nil && s.code != nil && repo.OwnerName != "" {
		for _, c := range commits {
			user, err := userByAuthorEmail(ctx, s.users, c.AuthorEmail)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				slog.Warn("post-receive: contributor lookup by email failed",
					"repo_id", repo.ID, "sha", c.SHA, "email", c.AuthorEmail, "error", err)
				continue
			}
			if user == nil {
				continue
			}
			detail, err := s.code.GetCommit(repo.OwnerName, repo.Name, c.SHA)
			if err != nil {
				slog.Warn("post-receive: load commit detail for contributor stats failed",
					"repo_id", repo.ID, "sha", c.SHA, "error", err)
				continue
			}
			if err := s.contributorStats.IngestCommit(ctx, repo.ID, user.ID, c.AuthorTime, c.SHA,
				detail.TotalAdded, detail.TotalDeleted); err != nil {
				slog.Warn("post-receive: contributor stats ingest failed",
					"repo_id", repo.ID, "sha", c.SHA, "user_id", user.ID, "error", err)
			}
		}
	}

	if s.pulls != nil {
		for _, cmd := range commands {
			if cmd == nil || !strings.HasPrefix(cmd.Name.String(), "refs/heads/") || cmd.Action() == packp.Delete {
				continue
			}
			branch := strings.TrimPrefix(cmd.Name.String(), "refs/heads/")
			if err := s.pulls.UpdateHeadSHAByBranch(ctx, repo.ID, branch, cmd.New.String()); err != nil {
				slog.Warn("post-receive: update PR head sha failed",
					"repo_id", repo.ID, "branch", branch, "error", err)
			}
		}
	}

	if s.language != nil && repo.OwnerName != "" && repo.DefaultBranch != "" {
		top, err := s.language.TopLanguageFor(ctx, repo, repo.DefaultBranch)
		if err != nil {
			slog.Warn("post-receive: language composition failed", "repo_id", repo.ID, "error", err)
		} else if err := s.repos.UpdatePrimaryLanguage(ctx, repo.ID, top); err != nil {
			slog.Error("post-receive: update primary language failed", "repo_id", repo.ID, "error", err)
		}
	}

	return nil
}

const (
	pushSummaryCommitCap = 3  // commits listed per push activity row
	pushSummaryWalkCap   = 50 // bounds the walk so a new-branch push doesn't count all of history
)

// PushSummaries walks the commits introduced by each updated branch and returns
// one summary per branch, for recording push activity-feed events.
func (s *RepoService) PushSummaries(gitRepo *gogit.Repository, commands []*packp.Command) []model.PushSummary {
	if gitRepo == nil {
		return nil
	}
	var summaries []model.PushSummary
	for _, cmd := range commands {
		if cmd == nil || !strings.HasPrefix(cmd.Name.String(), "refs/heads/") || cmd.Action() == packp.Delete {
			continue
		}
		var commits []model.CommitSummary
		total := 0
		walkErr := forEachPushedCommit(gitRepo, cmd, func(c *object.Commit) error {
			if total >= pushSummaryWalkCap {
				return storer.ErrStop
			}
			total++
			if len(commits) < pushSummaryCommitCap {
				commits = append(commits, model.CommitSummary{
					SHA:     c.Hash.String()[:7],
					Message: commitSubject(c.Message),
				})
			}
			return nil
		})
		if walkErr != nil {
			slog.Warn("push summary: commit walk failed", "ref", cmd.Name.String(), "error", walkErr)
		}
		if total == 0 {
			continue
		}
		summaries = append(summaries, model.PushSummary{
			Branch:      strings.TrimPrefix(cmd.Name.String(), "refs/heads/"),
			CommitTotal: total,
			Commits:     commits,
		})
	}
	return summaries
}

// forEachPushedCommit calls fn for each commit cmd adds to its branch until fn
// returns storer.ErrStop. A new branch has no old tip to stop at, so fn sees
// all of its history.
func forEachPushedCommit(gitRepo *gogit.Repository, cmd *packp.Command, fn func(*object.Commit) error) error {
	if cmd.Action() == packp.Create {
		iter, err := gitRepo.Log(&gogit.LogOptions{From: cmd.New})
		if err != nil {
			return err
		}
		defer iter.Close()
		return iter.ForEach(fn)
	}
	commits, err := commitRange(gitRepo, cmd.Old, cmd.New)
	if err != nil {
		return err
	}
	for _, c := range commits {
		err := fn(c)
		if errors.Is(err, storer.ErrStop) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func commitSubject(message string) string {
	line, _, _ := strings.Cut(message, "\n")
	return strings.TrimSpace(line)
}

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

func (s *RepoService) Create(ctx context.Context, ownerID int64, ownerUsername, name, description string, private bool, init RepoInitOptions) (*model.Repository, error) {
	if err := ValidateRepoName(name); err != nil {
		return nil, fmt.Errorf("invalid repository name: %w", err)
	}

	owner, err := s.personalOwner(ctx, ownerID, ownerUsername)
	if err != nil {
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
	if _, err := gogit.PlainInit(repoPath, true); err != nil {
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

	return r, nil
}

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

	workDir, err := os.MkdirTemp("", "cz-repo-init-*")
	if err != nil {
		return fmt.Errorf("mkdir temp worktree: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	work, err := gogit.PlainInit(workDir, false)
	if err != nil {
		return fmt.Errorf("git init worktree: %w", err)
	}
	wt, err := work.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}

	for relPath, content := range files {
		full := filepath.Join(workDir, relPath)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", relPath, err)
		}
		if _, err := wt.Add(relPath); err != nil {
			return fmt.Errorf("add %s: %w", relPath, err)
		}
	}

	if _, err := wt.Commit("Initial commit", &gogit.CommitOptions{Author: &sig, Committer: &sig}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	branch := defaultBranch
	if branch == "" {
		branch = "main"
	}

	if _, err := work.CreateRemote(&gitconfig.RemoteConfig{
		Name: "bare",
		URLs: []string{bareDir},
	}); err != nil {
		return fmt.Errorf("create remote: %w", err)
	}
	// Resolve the worktree's actual HEAD branch rather than assuming go-git's
	// PlainInit default ("master"), so the push survives a go-git default change.
	headRefAfterCommit, err := work.Head()
	if err != nil {
		return fmt.Errorf("resolve worktree HEAD: %w", err)
	}
	refSpec := gitconfig.RefSpec(headRefAfterCommit.Name().String() + ":" + plumbing.NewBranchReferenceName(branch).String())
	if err := work.Push(&gogit.PushOptions{
		RemoteName: "bare",
		RefSpecs:   []gitconfig.RefSpec{refSpec},
	}); err != nil {
		return fmt.Errorf("push to bare: %w", err)
	}

	bare, err := gogit.PlainOpen(bareDir)
	if err != nil {
		return fmt.Errorf("open bare: %w", err)
	}
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(branch))
	if err := bare.Storer.SetReference(headRef); err != nil {
		return fmt.Errorf("set bare HEAD: %w", err)
	}

	return nil
}

// ListVisibleTo is CanRead applied to every repo; viewerID is nil for anonymous viewers.
func (s *RepoService) ListVisibleTo(ctx context.Context, viewerID *int64) ([]model.Repository, error) {
	if viewerID == nil {
		return s.repos.ListPublic(ctx)
	}
	return s.repos.ListReadableBy(ctx, *viewerID)
}

func (s *RepoService) CountForUser(ctx context.Context, userID int64) (int, error) {
	return s.repos.CountForUser(ctx, userID)
}

// scope is "owned", "collaborator", or "all" (default for any unknown value).
func (s *RepoService) ListForUser(ctx context.Context, userID int64, scope string) ([]model.Repository, error) {
	if scope != "owned" && scope != "collaborator" {
		scope = "all"
	}
	return s.repos.ListForUser(ctx, userID, scope)
}

func (s *RepoService) GetByID(ctx context.Context, id int64) (*model.Repository, error) {
	return s.repos.GetByID(ctx, id)
}

func (s *RepoService) FillPrimaryLanguage(ctx context.Context, repoID int64, lang string) error {
	return s.repos.FillPrimaryLanguage(ctx, repoID, lang)
}

func (s *RepoService) Get(ctx context.Context, owner, name string) (*model.Repository, error) {
	repo, err := s.repos.GetByOwnerName(ctx, owner, name)
	if err != nil {
		return nil, err
	}
	s.enrichForkInfo(ctx, repo)
	return repo, nil
}

func (s *RepoService) enrichForkInfo(ctx context.Context, repo *model.Repository) {
	if repo.ForkOfID == nil {
		return
	}
	orig, err := s.repos.GetByID(ctx, *repo.ForkOfID)
	if err != nil {
		return
	}
	repo.ForkOfOwner = orig.OwnerName
	repo.ForkOfName = orig.Name
}

func (s *RepoService) ListByOwner(ctx context.Context, ownerUsername string) ([]model.Repository, error) {
	return s.repos.GetByOwnerNameList(ctx, ownerUsername)
}

// ListByOwnerVisibleTo returns the owner's repositories filtered to those the
// viewer is allowed to see: public repos plus any private repo the viewer owns,
// administers as an org owner, or has been granted a collaborator role on.
// Pass nil for viewerID for anonymous viewers.
func (s *RepoService) ListByOwnerVisibleTo(ctx context.Context, ownerUsername string, viewerID *int64) ([]model.Repository, error) {
	repos, err := s.repos.GetByOwnerNameList(ctx, ownerUsername)
	if err != nil {
		return nil, err
	}
	visible := make([]model.Repository, 0, len(repos))
	for i := range repos {
		if s.CanRead(ctx, &repos[i], viewerID) {
			visible = append(visible, repos[i])
		}
	}
	return visible, nil
}

func (s *RepoService) isOrgOwner(ctx context.Context, orgID, userID int64) bool {
	if orgID == 0 {
		return false
	}
	m, err := s.orgs.GetMember(ctx, orgID, userID)
	if err != nil {
		return false
	}
	return m.Role == model.OrgRoleOwner
}

func (s *RepoService) CanRead(ctx context.Context, repo *model.Repository, userID *int64) bool {
	if !repo.Private {
		return true
	}

	if userID == nil {
		return false
	}

	if s.IsOwner(ctx, repo, *userID) {
		return true
	}

	role, err := s.repos.GetPermission(ctx, repo.ID, *userID)
	if err != nil || role == "" {
		return false
	}

	return true
}

func (s *RepoService) CanWrite(ctx context.Context, repo *model.Repository, userID int64) bool {
	if s.IsOwner(ctx, repo, userID) {
		return true
	}

	role, err := s.repos.GetPermission(ctx, repo.ID, userID)
	if err != nil || role == "" {
		return false
	}

	return role == string(model.RoleWriter) || role == string(model.RoleAdmin)
}

// CanManage returns true for the repo owner, org owner, or admin collaborators.
// Grants access to manage collaborators, settings, branch protection, deploy keys, etc.
// Does NOT grant transfer, delete or the admin role — use IsOwner for those.
func (s *RepoService) CanManage(ctx context.Context, repo *model.Repository, userID int64) bool {
	if s.IsOwner(ctx, repo, userID) {
		return true
	}
	role, err := s.repos.GetPermission(ctx, repo.ID, userID)
	if err != nil || role == "" {
		return false
	}
	return role == string(model.RoleAdmin)
}

// IsOwner returns true only for the owner of a personal repo or an owner of
// an org repo's org; having created an org repo counts for nothing.
// Used for destructive operations: transfer, delete, archive, unarchive, template toggle,
// and for granting, changing or removing the admin role.
func (s *RepoService) IsOwner(ctx context.Context, repo *model.Repository, userID int64) bool {
	if repo.OrgID != 0 {
		return s.isOrgOwner(ctx, repo.OrgID, userID)
	}
	return repo.OwnerID == userID
}

func (s *RepoService) ListPermissionsByUser(ctx context.Context, userID int64) ([]model.Permission, error) {
	return s.repos.ListPermissionsByUser(ctx, userID)
}

func (s *RepoService) ListCollaborators(ctx context.Context, repoID int64) ([]model.Permission, error) {
	perms, err := s.repos.ListPermissionsWithUsername(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if perms == nil {
		perms = []model.Permission{}
	}
	return perms, nil
}

var (
	ErrInvalidCollaboratorRole = errors.New("role must be reader, writer or admin")
	ErrAdminRoleOwnerOnly      = errors.New("only the repository owner can grant, change or remove the admin role")
)

// AddCollaborator grants username role on repo, or changes the role they hold.
func (s *RepoService) AddCollaborator(ctx context.Context, repo *model.Repository, requestingUserID int64, username string, role string) error {
	switch model.Role(role) {
	case model.RoleReader, model.RoleWriter, model.RoleAdmin:
	default:
		return ErrInvalidCollaboratorRole
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	if err := s.checkAdminRoleChange(ctx, repo, requestingUserID, user.ID, model.Role(role)); err != nil {
		return err
	}
	return s.repos.AddPermission(ctx, repo.ID, user.ID, role)
}

func (s *RepoService) RemoveCollaborator(ctx context.Context, repo *model.Repository, requestingUserID, userID int64) error {
	if err := s.checkAdminRoleChange(ctx, repo, requestingUserID, userID, ""); err != nil {
		return err
	}
	return s.repos.RemovePermission(ctx, repo.ID, userID)
}

// checkAdminRoleChange lets only an owner give userID the admin role, or change
// or remove one userID holds; an empty newRole is a removal. An admin who could
// appoint admins could plant a second account that outlives their own removal.
func (s *RepoService) checkAdminRoleChange(ctx context.Context, repo *model.Repository, requestingUserID, userID int64, newRole model.Role) error {
	if s.IsOwner(ctx, repo, requestingUserID) {
		return nil
	}
	if newRole == model.RoleAdmin {
		return ErrAdminRoleOwnerOnly
	}
	current, err := s.repos.GetPermission(ctx, repo.ID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if model.Role(current) == model.RoleAdmin {
		return ErrAdminRoleOwnerOnly
	}
	return nil
}

// Fork creates a copy of originalOwner/originalName under the actor's namespace.
func (s *RepoService) Fork(ctx context.Context, originalOwner, originalName string, actorID int64, actorUsername string) (*model.Repository, error) {
	orig, err := s.repos.GetByOwnerName(ctx, originalOwner, originalName)
	if err != nil {
		return nil, fmt.Errorf("original repo not found: %w", err)
	}

	if !s.CanRead(ctx, orig, &actorID) {
		return nil, fmt.Errorf("access denied")
	}
	if _, err := s.personalOwner(ctx, actorID, actorUsername); err != nil {
		return nil, err
	}

	var forkName, dstPath string
	for i := 0; ; i++ {
		forkName = originalName
		if i > 0 {
			forkName = fmt.Sprintf("%s-%d", originalName, i)
		}
		dstPath, err = claimRepo(ctx, s.repos, s.cfg.ReposRoot, actorUsername, forkName)
		if !errors.Is(err, ErrRepoNameTaken) && !errors.Is(err, ErrRepoNameReserved) {
			break
		}
	}
	if err != nil {
		return nil, err
	}

	forked, err := s.repos.Fork(ctx, orig, actorID, actorUsername, forkName)
	if err != nil {
		abandonNewRepo(ctx, s.repos, 0, dstPath)
		return nil, repoNameErr("fork db record", err)
	}

	srcPath, _ := repoDirs(s.cfg.ReposRoot, originalOwner, originalName)
	if err := copyDir(srcPath, dstPath); err != nil {
		abandonNewRepo(ctx, s.repos, forked.ID, dstPath)
		return nil, fmt.Errorf("copy git dir: %w", err)
	}

	_ = s.repos.IncrementForkCount(ctx, orig.ID)

	forked.ForkOfOwner = originalOwner
	forked.ForkOfName = originalName
	return forked, nil
}

// copyDir recursively copies src directory to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// archiveGuard returns ErrForbidden if the caller is not an owner.
func archiveGuard(isOwner bool) error {
	if !isOwner {
		return fmt.Errorf("only the repo owner or org owner can archive a repo: %w", ErrForbidden)
	}
	return nil
}

// templateGuard returns ErrForbidden if the caller is not an owner.
func templateGuard(isOwner bool) error {
	if !isOwner {
		return fmt.Errorf("only the repo owner or org owner can change template status: %w", ErrForbidden)
	}
	return nil
}

func (s *RepoService) Archive(ctx context.Context, repoID, userID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := archiveGuard(s.IsOwner(ctx, repo, userID)); err != nil {
		return err
	}
	return s.repos.SetArchived(ctx, repoID, true)
}

func (s *RepoService) Unarchive(ctx context.Context, repoID, userID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := archiveGuard(s.IsOwner(ctx, repo, userID)); err != nil {
		return err
	}
	return s.repos.SetArchived(ctx, repoID, false)
}

func (s *RepoService) SetTemplate(ctx context.Context, repoID, userID int64, isTemplate bool) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := templateGuard(s.IsOwner(ctx, repo, userID)); err != nil {
		return err
	}
	return s.repos.SetTemplate(ctx, repoID, isTemplate)
}

// UpdateMeta updates the user-editable repository metadata (description,
// website, license). Requires manage permission on the repo.
func (s *RepoService) UpdateMeta(ctx context.Context, repoID, userID int64, description, website, license string) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.CanManage(ctx, repo, userID) {
		return fmt.Errorf("manage permission required: %w", ErrForbidden)
	}
	return s.repos.UpdateMeta(ctx, repoID, strings.TrimSpace(description), strings.TrimSpace(website), strings.TrimSpace(license))
}

// UpdateGeneral updates the description, website, and default branch. Requires
// manage permission; an empty defaultBranch leaves the current one unchanged.
func (s *RepoService) UpdateGeneral(ctx context.Context, repoID, userID int64, description, website, defaultBranch string) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.CanManage(ctx, repo, userID) {
		return fmt.Errorf("manage permission required: %w", ErrForbidden)
	}
	branch := strings.TrimSpace(defaultBranch)
	if branch == "" {
		branch = repo.DefaultBranch
	}
	return s.repos.UpdateGeneral(ctx, repoID, strings.TrimSpace(description), strings.TrimSpace(website), branch)
}

// UpdateFeatureToggles updates the Issues/Discussions/Projects/Wiki feature
// flags. Requires manage permission.
func (s *RepoService) UpdateFeatureToggles(ctx context.Context, repoID, userID int64, issues, discussions, projects, wiki bool) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.CanManage(ctx, repo, userID) {
		return fmt.Errorf("manage permission required: %w", ErrForbidden)
	}
	return s.repos.UpdateFeatureToggles(ctx, repoID, issues, discussions, projects, wiki)
}

func (s *RepoService) UpdateVisibility(ctx context.Context, repoID, userID int64, private bool) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if !s.CanManage(ctx, repo, userID) {
		return fmt.Errorf("manage permission required: %w", ErrForbidden)
	}
	return s.repos.UpdateVisibility(ctx, repoID, private)
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
	} else if _, err := gogit.PlainInit(dstPath, true); err != nil {
		abandonNewRepo(ctx, s.repos, newRepo.ID, dstPath)
		return nil, fmt.Errorf("git init bare for template copy: %w", err)
	}

	return newRepo, nil
}

func (s *RepoService) ListTemplates(ctx context.Context) ([]model.Repository, error) {
	return s.repos.ListTemplates(ctx)
}

// deleteGuard returns ErrForbidden if the caller is not an owner.
func deleteGuard(isOwner bool) error {
	if !isOwner {
		return fmt.Errorf("only the repo owner or org owner can delete a repo: %w", ErrForbidden)
	}
	return nil
}

func (s *RepoService) Delete(ctx context.Context, repoID, userID int64) error {
	repo, err := s.repos.GetByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := deleteGuard(s.IsOwner(ctx, repo, userID)); err != nil {
		return err
	}

	// deleted_at carries the suffix's second so Restore and purge find this
	// row's copy among other holders' copies of the name.
	now := time.Now()
	gitDir, _ := repoDirs(s.cfg.ReposRoot, repo.OwnerName, repo.Name)
	dirs := []string{gitDir}
	wikiDir, err := s.ownWikiDir(ctx, repo.OwnerName, repo.Name)
	if err != nil {
		return err
	}
	if wikiDir != "" {
		dirs = append(dirs, wikiDir)
	}
	moved, err := renameDirs(movesAside(deletedSuffix(now), dirs...))
	if err != nil {
		return fmt.Errorf("rename git dir for soft delete: %w", err)
	}
	if err := s.repos.Delete(ctx, repoID, repo.OwnerName, userID, now); err != nil {
		revertDirs(moved)
		return err
	}
	return nil
}

func (s *RepoService) Restore(ctx context.Context, repoID, requesterID int64, isSuperadmin bool) error {
	repo, err := s.repos.GetDeletedByID(ctx, repoID)
	if err != nil {
		return fmt.Errorf("deleted repo not found: %w", err)
	}
	if !isSuperadmin && !s.IsOwner(ctx, repo, requesterID) {
		return fmt.Errorf("forbidden: only the repo's owner or a superadmin can restore a repo")
	}

	// A soft-deleted org repo does not hold its name, so the name may have a new
	// holder even when this row's copy is gone; restoring beside it would share
	// its dirs.
	// The live wiki path is not checked: repos deleted before wikis moved with
	// them left theirs there, and renameDirs never overwrites one.
	gitDir, wikiDir := repoDirs(s.cfg.ReposRoot, repo.OwnerName, repo.Name)
	_, err = s.repos.GetByOwnerName(ctx, repo.OwnerName, repo.Name)
	switch {
	case err == nil, pathTaken(gitDir):
		return fmt.Errorf("restore conflict: %w", ErrRepoNameTaken)
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	suffix, ok := deletedCopySuffix(s.cfg.ReposRoot, *repo)
	if !ok {
		return fmt.Errorf("restore: no soft-deleted copy of %s/%s on disk", repo.OwnerName, repo.Name)
	}
	restored, err := renameDirs([]dirMove{{from: gitDir + suffix, to: gitDir}, {from: wikiDir + suffix, to: wikiDir}})
	if err != nil {
		return fmt.Errorf("rename git dir back on restore: %w", err)
	}

	if err := s.repos.Restore(ctx, repoID); err != nil {
		revertDirs(restored)
		return err
	}
	return nil
}

func (s *RepoService) GetDeleted(ctx context.Context, ownerName, name string) (*model.Repository, error) {
	return s.repos.GetDeletedByOwnerAndName(ctx, ownerName, name)
}

func (s *RepoService) PurgeExpired(ctx context.Context) error {
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	expired, err := s.repos.PurgeExpired(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("purge expired repos: %w", err)
	}
	for _, r := range expired {
		removeDeletedCopy(s.cfg.ReposRoot, r)
		s.removeStrandedWiki(ctx, r.OwnerName, r.Name)
	}
	return nil
}
