package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

var ErrInvalidRepoName = errors.New("invalid repository name")

var ErrInvalidDefaultBranch = errors.New("invalid default branch")

var (
	ErrRepoArchived = errors.New("repository is archived")
	ErrRepoMirror   = errors.New("repository is a pull mirror")
)

// CheckContentWritable refuses any change to a repo's refs or commits. Every
// git-content write calls it, whatever the caller's role.
func CheckContentWritable(repo *model.Repository) error {
	switch {
	case repo.IsArchived:
		return ErrRepoArchived
	case repo.IsMirror:
		return ErrRepoMirror
	}
	return nil
}

// PushRefusal is what a git client shows for a push CheckContentWritable refuses.
func PushRefusal(err error) string {
	if errors.Is(err, ErrRepoMirror) {
		return "Repository is a mirror and is read-only."
	}
	return "Repository is archived and read-only."
}

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
	quota            *QuotaService
	attachments      *AttachmentService
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

// WithAttachments removes a purged repo's attachments.
func (s *RepoService) WithAttachments(a *AttachmentService) *RepoService {
	s.attachments = a
	return s
}

func (s *RepoService) WithLanguageService(lang *LanguageService) *RepoService {
	s.language = lang
	return s
}

func (s *RepoService) WithQuota(q *QuotaService) *RepoService {
	s.quota = q
	return s
}

// CheckNewRepoQuota lets a caller refuse a repo for t before doing the work of creating it.
func (s *RepoService) CheckNewRepoQuota(ctx context.Context, t RepoTarget) error {
	return s.quota.CheckNewRepo(ctx, targetQuotaOwner(t))
}

func (s *RepoService) WithPullStore(pulls *store.PullStore) *RepoService {
	s.pulls = pulls
	return s
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
