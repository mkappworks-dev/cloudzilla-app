package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretbox"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const mirrorCredentialPurpose = "mirror-credential"

// Not a mirror's +refs/*:refs/*, which would also copy GitHub's refs/pull/*.
var mirrorRefSpecs = []gitconfig.RefSpec{"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"}

var ErrMirrorNoSecretKey = errors.New("mirroring with credentials needs security.secret_key")

// MirrorSyncError is a failed sync, worded for the repo's admins.
type MirrorSyncError struct {
	Msg string
	Err error
}

func (e *MirrorSyncError) Error() string { return e.Msg }
func (e *MirrorSyncError) Unwrap() error { return e.Err }

// MirrorService keeps pull mirrors in step with their upstreams.
type MirrorService struct {
	mirrors   *store.MirrorStore
	repoStore *store.RepoStore
	repos     *RepoService
	webhooks  *WebhookService
	index     *IndexService
	deps      *DependencyService
	secrets   *secretbox.Box
	git       config.GitConfig
	cfg       config.MirrorConfig
}

func NewMirrorService(mirrors *store.MirrorStore, repoStore *store.RepoStore, repos *RepoService, webhooks *WebhookService,
	index *IndexService, deps *DependencyService, secrets *secretbox.Box, git config.GitConfig, cfg config.MirrorConfig) *MirrorService {
	return &MirrorService{mirrors: mirrors, repoStore: repoStore, repos: repos, webhooks: webhooks, index: index, deps: deps,
		secrets: secrets, git: git, cfg: cfg}
}

// SealToken encrypts an upstream token for storage.
func (s *MirrorService) SealToken(token string) ([]byte, error) {
	sealed, err := s.secrets.Seal(mirrorCredentialPurpose, []byte(token))
	if errors.Is(err, secretbox.ErrNoKey) {
		return nil, ErrMirrorNoSecretKey
	}
	return sealed, err
}

// Sync fetches the upstream's branches and tags into the mirror, pruning
// what the upstream dropped, follows its default branch, and runs the
// side effects of a push for the refs that moved. Failures the admins can
// act on are a *MirrorSyncError.
func (s *MirrorService) Sync(ctx context.Context, m *model.RepoMirror) error {
	installImportTransport()
	repo, err := s.repoStore.GetByID(ctx, m.RepoID)
	if err != nil {
		return fmt.Errorf("mirror sync: load repo %d: %w", m.RepoID, err)
	}
	auth, err := s.auth(m)
	if err != nil {
		return err
	}
	gitDir, err := RepoDir(s.git.ReposRoot, repo.OwnerName, repo.Name+".git")
	if err != nil {
		return err
	}
	local, err := gogit.PlainOpen(gitDir)
	if err != nil {
		return fmt.Errorf("mirror sync: open repo: %w", err)
	}

	guard := &importGuard{
		allowLocal:    s.cfg.AllowLocalNetworks,
		maxPackBytes:  s.git.MaxPackBytes,
		maxRefsBytes:  importMaxRefsBytes,
		maxErrorBytes: importMaxErrorBodyBytes,
	}
	ctx = withImportGuard(ctx, guard)
	before, err := mirroredRefs(local)
	if err != nil {
		return err
	}
	remote := gogit.NewRemote(local.Storer, &gitconfig.RemoteConfig{
		Name: "upstream", URLs: []string{m.RemoteURL}, Fetch: mirrorRefSpecs,
	})
	advertised, err := remote.ListContext(ctx, &gogit.ListOptions{Auth: auth})
	if err == nil {
		err = remote.FetchContext(ctx, &gogit.FetchOptions{Auth: auth, Tags: gogit.NoTags, Prune: true, Force: true})
	}
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return s.syncError(ctx, repo, guard, err)
	}
	after, err := mirroredRefs(local)
	if err != nil {
		return err
	}
	if err := s.followHead(ctx, repo, local, advertised); err != nil {
		return err
	}
	s.afterSync(ctx, repo, local, refChanges(before, after))
	return nil
}

func (s *MirrorService) auth(m *model.RepoMirror) (transport.AuthMethod, error) {
	if m.AuthTokenEnc == nil {
		return nil, nil
	}
	token, err := s.secrets.Open(mirrorCredentialPurpose, m.AuthTokenEnc)
	switch {
	case errors.Is(err, secretbox.ErrNoKey):
		return nil, &MirrorSyncError{Msg: "The stored token can't be read because security.secret_key is not set.", Err: err}
	case err != nil:
		return nil, &MirrorSyncError{Msg: "The stored token can't be decrypted; re-enter the token in the mirror settings.", Err: err}
	}
	return importAuth(m.AuthUsername, string(token)), nil
}

func (s *MirrorService) syncError(ctx context.Context, repo *model.Repository, guard *importGuard, err error) error {
	var blocked *PrivateNetworkError
	var tooLarge *importSizeError
	stopped := guard.failure()
	msg := ""
	switch {
	case errors.As(stopped, &blocked):
		msg = blocked.Host + " resolves to a private network address. An administrator can allow this with mirror.allow_local_networks."
	case errors.As(stopped, &tooLarge):
		msg = tooLarge.message()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		msg = "The sync took longer than " + formatImportTimeout(s.cfg.Timeout) + " and was stopped."
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed),
		errors.Is(err, transport.ErrRepositoryNotFound):
		msg = "The source repository was not found, or the stored credentials were rejected."
	case errors.Is(err, transport.ErrEmptyRemoteRepository):
		msg = "The source repository is empty. The mirror keeps its last copy."
	default:
		slog.Error("mirror sync failed", "repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
		msg = "The sync failed. Check that the source repository is reachable."
	}
	return &MirrorSyncError{Msg: msg, Err: err}
}

// followHead points HEAD and the default branch where the upstream's point,
// choosing as an import does when the upstream doesn't say.
func (s *MirrorService) followHead(ctx context.Context, repo *model.Repository, local *gogit.Repository, advertised []*plumbing.Reference) error {
	branch, err := defaultImportBranch(advertised)
	if err != nil {
		return nil
	}
	target := plumbing.NewBranchReferenceName(branch)
	if head, err := local.Storer.Reference(plumbing.HEAD); err != nil || head.Target() != target {
		if err := local.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, target)); err != nil {
			return fmt.Errorf("mirror sync: set HEAD: %w", err)
		}
	}
	if branch != repo.DefaultBranch {
		if err := s.repoStore.SetDefaultBranch(ctx, repo.ID, branch); err != nil {
			return err
		}
		repo.DefaultBranch = branch
	}
	return nil
}

// afterSync runs what a push of cmds would, minus activity events and
// notifications: a sync has no actor, and an upstream's commits are no one
// here's news.
func (s *MirrorService) afterSync(ctx context.Context, repo *model.Repository, local *gogit.Repository, cmds []*packp.Command) {
	if len(cmds) == 0 {
		return
	}
	for _, cmd := range cmds {
		if !cmd.Name.IsBranch() || cmd.Action() == packp.Delete {
			continue
		}
		s.webhooks.Dispatch(repo.ID, "push", s.webhooks.PushPayload(*repo, "", cmd.Name.Short(), cmd.New.String()))
	}
	if err := s.repos.OnPostReceive(ctx, repo, local, cmds); err != nil {
		slog.Error("mirror sync: post-receive failed", "repo_id", repo.ID, "error", err)
	}
	if err := s.index.IndexRepo(ctx, repo); err != nil {
		slog.Error("mirror sync: re-index failed", "repo_id", repo.ID, "error", err)
	}
	if err := s.deps.ParseAndStore(ctx, repo); err != nil {
		slog.Error("mirror sync: dependency parse failed", "repo_id", repo.ID, "error", err)
	}
}

// mirroredRefs maps the branches and tags a sync manages to their hashes.
func mirroredRefs(repo *gogit.Repository) (map[plumbing.ReferenceName]plumbing.Hash, error) {
	iter, err := repo.Storer.IterReferences()
	if err != nil {
		return nil, fmt.Errorf("mirror sync: list refs: %w", err)
	}
	refs := map[plumbing.ReferenceName]plumbing.Hash{}
	err = iter.ForEach(func(r *plumbing.Reference) error {
		if r.Type() == plumbing.HashReference && (r.Name().IsBranch() || r.Name().IsTag()) {
			refs[r.Name()] = r.Hash()
		}
		return nil
	})
	return refs, err
}

// refChanges is the push that would have turned before into after.
func refChanges(before, after map[plumbing.ReferenceName]plumbing.Hash) []*packp.Command {
	var cmds []*packp.Command
	for name, old := range before {
		if after[name] != old {
			cmds = append(cmds, &packp.Command{Name: name, Old: old, New: after[name]})
		}
	}
	for name, h := range after {
		if _, ok := before[name]; !ok {
			cmds = append(cmds, &packp.Command{Name: name, Old: plumbing.ZeroHash, New: h})
		}
	}
	sort.Slice(cmds, func(i, j int) bool { return strings.Compare(cmds[i].Name.String(), cmds[j].Name.String()) < 0 })
	return cmds
}
