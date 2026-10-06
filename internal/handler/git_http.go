package handler

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func validGitName(name string) bool {
	return service.ValidateName(name) == nil
}

type gitUser struct {
	ID       int64
	Username string
	Targets  []string // the repos and orgs the user's token is limited to, if any
}

// resolveGitUser returns the authenticated user for git operations, or nil for an
// anonymous request. It checks claims first, set by the auth middleware after it
// enforced token scopes, then HTTP Basic Auth whose password is a PAT (git CLI:
// username:czp_xxx). Auth never sees a Basic PAT, so its scopes are
// enforced here: the error names the scope the token lacks for r. Callers answer
// it with 403, not 401, because on a 401 git's credential helper erases the token.
func (h *Handler) resolveGitUser(r *http.Request) (*gitUser, error) {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		return &gitUser{ID: claims.UserID, Username: claims.Username, Targets: claims.Targets}, nil
	}
	_, password, ok := r.BasicAuth()
	if !ok || !strings.HasPrefix(password, "czp_") {
		return nil, nil
	}
	token, user, err := middleware.ValidatePAT(r, h.Services.AccessToken, password)
	if errors.Is(err, service.ErrAccountSuspended) {
		return nil, err
	}
	if err != nil {
		return nil, nil
	}
	if token.SigningKey != "" {
		return nil, fmt.Errorf("personal access token %q is bound to a signing key, which git can't sign with; use a token without one", token.Name)
	}
	tokenID := token.ID
	concurrency.Go("access_token.update_last_used", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.Services.AccessToken.UpdateLastUsed(ctx, tokenID); err != nil {
			slog.Warn("access token last_used update failed", "token_id", tokenID, "error", err)
		}
	})
	if !middleware.ScopeAllows(middleware.PATClaims(token, user), r) {
		return nil, fmt.Errorf("personal access token lacks the %s scope", middleware.RequiredScope(r))
	}
	return &gitUser{ID: user.ID, Username: user.Username, Targets: token.Targets}, nil
}

func gitAuthChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
	http.Error(w, "authentication required", http.StatusUnauthorized)
}

// gitReadableRepo resolves the caller and loads a repo they may read. Everything
// that can fail without revealing the repo (path, token) is checked first, and a
// caller who can't read the repo gets exactly what a missing repo gets: a 401
// challenge when anonymous, so git prompts for credentials, otherwise a 404.
func (h *Handler) gitReadableRepo(w http.ResponseWriter, r *http.Request, owner, repoName string) (*model.Repository, *gitUser, bool) {
	if !validGitName(owner) || !validGitName(repoName) {
		http.Error(w, "invalid repository path", http.StatusBadRequest)
		return nil, nil, false
	}
	gu, err := h.resolveGitUser(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return nil, nil, false
	}
	var uid *int64
	if gu != nil {
		uid = &gu.ID
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err == nil && h.Services.Repo.CanRead(r.Context(), repo, uid) {
		return repo, gu, true
	}
	if gu == nil {
		gitAuthChallenge(w)
	} else {
		http.Error(w, "repository not found", http.StatusNotFound)
	}
	return nil, nil, false
}

// gitCanPush answers a push the caller may not make. A reader gets a 403, not a
// 401, so git's credential helper keeps their credential.
func (h *Handler) gitCanPush(w http.ResponseWriter, r *http.Request, repo *model.Repository, gu *gitUser) bool {
	switch {
	case gu == nil:
		gitAuthChallenge(w)
	case !h.Services.Repo.CanWrite(r.Context(), repo, gu.ID):
		http.Error(w, "access denied", http.StatusForbidden)
	case repo.IsArchived:
		http.Error(w, "Repository is archived and read-only.\n", http.StatusForbidden)
	default:
		return true
	}
	return false
}

func (h *Handler) GitInfoRefs(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := strings.TrimSuffix(chi.URLParam(r, "repo"), ".git")

	svc := r.URL.Query().Get("service")
	if svc != "git-upload-pack" && svc != "git-receive-pack" {
		http.Error(w, "invalid service", http.StatusBadRequest)
		return
	}

	repo, gu, ok := h.gitReadableRepo(w, r, owner, repoName)
	if !ok {
		return
	}
	if svc == "git-receive-pack" && !h.gitCanPush(w, r, repo, gu) {
		return
	}

	repoPath, err := service.RepoDir(h.Cfg.Git.ReposRoot, owner, repoName+".git")
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}
	gitRepo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		http.Error(w, "failed to open repository", http.StatusInternalServerError)
		return
	}

	ep, err := transport.NewEndpoint("/")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// MapLoader is keyed on ep.String() (e.g. "file:///"), not the input to NewEndpoint.
	srv := server.NewServer(server.MapLoader{ep.String(): gitRepo.Storer})

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-advertisement", strings.TrimPrefix(svc, "git-")))

	// Smart-HTTP preamble required before the advertisement.
	pe := pktline.NewEncoder(w)
	if err := pe.Encodef("# service=%s\n", svc); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := pe.Flush(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if svc == "git-upload-pack" {
		sess, err := srv.NewUploadPackSession(ep, nil)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		ar, err := sess.AdvertisedReferences()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		ar.Encode(w) //nolint:errcheck
	} else {
		sess, err := srv.NewReceivePackSession(ep, nil)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		ar, err := sess.AdvertisedReferences()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		ar.Encode(w) //nolint:errcheck
	}
}

func (h *Handler) GitUploadPack(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := strings.TrimSuffix(chi.URLParam(r, "repo"), ".git")

	if _, _, ok := h.gitReadableRepo(w, r, owner, repoName); !ok {
		return
	}

	repoPath, err := service.RepoDir(h.Cfg.Git.ReposRoot, owner, repoName+".git")
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}
	gitRepo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		http.Error(w, "failed to open repository", http.StatusInternalServerError)
		return
	}

	// Handle gzip-encoded bodies
	body := io.Reader(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(body)
		if err != nil {
			http.Error(w, "failed to decompress", http.StatusBadRequest)
			return
		}
		defer func() { _ = gr.Close() }()
		body = gr
	}

	ep, err := transport.NewEndpoint("/")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	srv := server.NewServer(server.MapLoader{ep.String(): gitRepo.Storer})
	sess, err := srv.NewUploadPackSession(ep, nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// AdvertisedReferences must be called to initialise session capabilities
	if _, err := sess.AdvertisedReferences(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	req := packp.NewUploadPackRequest()
	if err := req.Decode(body); err != nil {
		http.Error(w, "failed to decode request", http.StatusBadRequest)
		return
	}

	resp, err := sess.UploadPack(r.Context(), req)
	if err != nil {
		http.Error(w, "upload-pack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	if err := resp.Encode(w); err != nil {
		return
	}
}

func (h *Handler) GitReceivePack(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := strings.TrimSuffix(chi.URLParam(r, "repo"), ".git")

	repo, gu, ok := h.gitReadableRepo(w, r, owner, repoName)
	if !ok || !h.gitCanPush(w, r, repo, gu) {
		return
	}

	repoPath, err := service.RepoDir(h.Cfg.Git.ReposRoot, owner, repoName+".git")
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}
	gitRepo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		http.Error(w, "failed to open repository", http.StatusInternalServerError)
		return
	}

	// Handle gzip-encoded bodies
	body := io.ReadCloser(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "failed to decompress", http.StatusBadRequest)
			return
		}
		defer func() { _ = gr.Close() }()
		body = io.NopCloser(gr)
	}

	// Cap pack size after decompression, so the limit also bounds a gzip bomb.
	limiter := gittransport.NewLimitedReadCloser(body, h.Cfg.Git.MaxPackBytes)
	counter := gittransport.NewByteCounter(limiter)
	body = counter

	ep, err := transport.NewEndpoint("/")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	vet := func(cmd *packp.Command) error {
		return h.Services.BranchProtection.CheckPushCommand(r.Context(), repo.ID, gitRepo, cmd)
	}
	sess, err := gittransport.NewServer(gitRepo.Storer, vet).NewReceivePackSession(ep, nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// AdvertisedReferences must be called to initialise session capabilities
	if _, err := sess.AdvertisedReferences(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	req := packp.NewReferenceUpdateRequest()
	if err := req.Decode(body); err != nil {
		http.Error(w, "failed to decode request", http.StatusBadRequest)
		return
	}

	// go-git rejects empty command lists; treat as up-to-date.
	if len(req.Commands) == 0 {
		w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
		w.WriteHeader(http.StatusOK)
		return
	}

	start := time.Now()
	status, err := sess.ReceivePack(r.Context(), req)
	if err != nil {
		if limiter.Exceeded() {
			slog.Warn("git-http: receive-pack rejected: pack too large",
				"owner", owner, "repo", repoName, "limit_bytes", h.Cfg.Git.MaxPackBytes)
			http.Error(w, "pack exceeds maximum allowed size", http.StatusRequestEntityTooLarge)
			return
		}
		slog.Error("git-http: receive-pack failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "receive-pack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	refsOK, refsFailed := gittransport.CountRefStatus(status)
	slog.Info("git-http: receive-pack complete",
		"owner", owner,
		"repo", repoName,
		"pusher", gu.Username,
		"commands", len(req.Commands),
		"refs_ok", refsOK,
		"refs_failed", refsFailed,
		"pack_bytes", counter.Bytes(),
		"duration_ms", time.Since(start).Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	if status != nil && req.Capabilities.Supports(capability.ReportStatus) {
		status.Encode(w) //nolint:errcheck
	}

	// Run side effects only for refs go-git applied — a per-ref failure
	// surfaces in status, not as a ReceivePack error.
	commands := gittransport.AppliedCommands(status, req.Commands)

	// Dispatch push webhooks for each updated branch
	pusherName := gu.Username
	for _, cmd := range commands {
		if !strings.HasPrefix(cmd.Name.String(), "refs/heads/") {
			continue
		}
		if cmd.Action() == packp.Delete {
			continue
		}
		branch := strings.TrimPrefix(cmd.Name.String(), "refs/heads/")
		payload := h.Services.Webhook.PushPayload(*repo, pusherName, branch, cmd.New.String())
		repoID := repo.ID
		concurrency.Go("webhook.dispatch.push", func() {
			h.Services.Webhook.Dispatch(repoID, "push", payload)
		})
	}

	// Record push activity-feed events (one per updated branch).
	// Pushes with no human actor are skipped, matching the SSH path.
	if pusherName != "" {
		pusherID := gu.ID
		repoID := repo.ID
		concurrency.Go("event.record.push", func() {
			for _, ps := range h.Services.Repo.PushSummaries(gitRepo, commands) {
				h.Services.Event.RecordPush(context.Background(), pusherID, pusherName, &repoID, repoName, owner, ps)
			}
		})
	}

	if pusherName != "" {
		actor := service.CloseActor{UserID: gu.ID, Username: gu.Username, Targets: gu.Targets}
		concurrency.Go("issue_closer.close_for_push", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			h.Services.IssueCloser.CloseForPush(ctx, actor, repo, gitRepo, commands)
		})
	}

	concurrency.Go("repo.on_post_receive", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := h.Services.Repo.OnPostReceive(ctx, repo, gitRepo, commands); err != nil {
			slog.Error("post-receive: commit stats ingest failed",
				"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
		}
	})

	concurrency.Go("index.index_repo", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := h.Services.Index.IndexRepo(ctx, repo); err != nil {
			slog.Error("index: failed to re-index repo",
				"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
		}
	})

	concurrency.Go("dependency.parse_and_store", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := h.Services.Dependency.ParseAndStore(ctx, repo); err != nil {
			slog.Error("dependency: failed to parse and store manifests",
				"repo_id", repo.ID,
				"owner", repo.OwnerName,
				"repo", repo.Name,
				"error", err,
			)
		}
	})
}
