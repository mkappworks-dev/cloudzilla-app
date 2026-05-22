package handler

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func validGitName(name string) bool {
	return service.ValidateName(name) == nil
}

type gitUser struct {
	ID       int64
	Username string
}

// resolveGitUser returns the authenticated user for git operations.
// It checks JWT claims first (browser/cookie), then falls back to HTTP Basic Auth
// where the password is a PAT (git CLI: username:czp_xxx).
func (h *Handler) resolveGitUser(r *http.Request) *gitUser {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		return &gitUser{ID: claims.UserID, Username: claims.Username}
	}
	_, password, ok := r.BasicAuth()
	if ok && strings.HasPrefix(password, "czp_") {
		token, user, err := h.Services.AccessToken.Validate(r.Context(), password)
		if err == nil {
			tokenID := token.ID
			concurrency.Go("access_token.update_last_used", func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := h.Services.AccessToken.UpdateLastUsed(ctx, tokenID); err != nil {
					slog.Warn("access token last_used update failed", "token_id", tokenID, "error", err)
				}
			})
			return &gitUser{ID: user.ID, Username: user.Username}
		}
	}
	return nil
}

func (h *Handler) GitInfoRefs(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := strings.TrimSuffix(chi.URLParam(r, "repo"), ".git")

	if !validGitName(owner) || !validGitName(repoName) {
		http.Error(w, "invalid repository path", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	svc := r.URL.Query().Get("service")
	if svc != "git-upload-pack" && svc != "git-receive-pack" {
		http.Error(w, "invalid service", http.StatusBadRequest)
		return
	}

	gu := h.resolveGitUser(r)

	if svc == "git-receive-pack" {
		var uid *int64
		if gu != nil {
			uid = &gu.ID
		}
		if uid == nil || !h.Services.Repo.CanWrite(r.Context(), repo, *uid) {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "access denied", http.StatusUnauthorized)
			return
		}
		if repo.IsArchived {
			http.Error(w, "Repository is archived and read-only.\n", http.StatusForbidden)
			return
		}
	} else {
		var uid *int64
		if gu != nil {
			uid = &gu.ID
		}
		if !h.Services.Repo.CanRead(r.Context(), repo, uid) {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "access denied", http.StatusUnauthorized)
			return
		}
	}

	repoPath := filepath.Join(h.Cfg.Git.ReposRoot, owner, repoName+".git")
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

	if !validGitName(owner) || !validGitName(repoName) {
		http.Error(w, "invalid repository path", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	gu := h.resolveGitUser(r)
	var userID *int64
	if gu != nil {
		userID = &gu.ID
	}

	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	repoPath := filepath.Join(h.Cfg.Git.ReposRoot, owner, repoName+".git")
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
		defer gr.Close()
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

	if !validGitName(owner) || !validGitName(repoName) {
		http.Error(w, "invalid repository path", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	gu := h.resolveGitUser(r)
	if gu == nil || !h.Services.Repo.CanWrite(r.Context(), repo, gu.ID) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		http.Error(w, "access denied", http.StatusUnauthorized)
		return
	}

	if repo.IsArchived {
		http.Error(w, "Repository is archived and read-only.\n", http.StatusForbidden)
		return
	}

	repoPath := filepath.Join(h.Cfg.Git.ReposRoot, owner, repoName+".git")
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
		defer gr.Close()
		body = io.NopCloser(gr)
	}

	// Cap the pack size after decompression, so the limit bounds both an
	// oversized pack and a gzip bomb. The counter then reports the actual
	// pack payload (not the gzipped wire bytes) for observability.
	limiter := gittransport.NewLimitedReadCloser(body, h.Cfg.Git.MaxPackBytes)
	counter := gittransport.NewByteCounter(limiter)
	body = counter

	ep, err := transport.NewEndpoint("/")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// WrapForReceive routes receive-pack onto go-git's parsed-storage
	// path; the filesystem fast path can't resolve thin-pack REF_DELTAs.
	// See docs/git-transport.md → "Thin packs".
	srv := server.NewServer(server.MapLoader{
		ep.String(): gittransport.WrapForReceive(gitRepo.Storer),
	})
	sess, err := srv.NewReceivePackSession(ep, nil)
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
	if status != nil {
		status.Encode(w) //nolint:errcheck
	}

	// Enforce branch protection rules before dispatching webhooks.
	// If protection rejects the push, rollback the ref to its previous value.
	for _, cmd := range req.Commands {
		if !strings.HasPrefix(cmd.Name.String(), "refs/heads/") {
			continue
		}
		if cmd.Action() == packp.Delete {
			continue
		}
		branch := strings.TrimPrefix(cmd.Name.String(), "refs/heads/")
		forcePush := cmd.Action() == packp.Update && cmd.Old != plumbing.ZeroHash && isForcePushHTTP(gitRepo, cmd)
		if err := h.Services.BranchProtection.CheckPush(r.Context(), repo.ID, branch, forcePush); err != nil {
			// Rollback the ref to its previous value
			ref := plumbing.NewHashReference(cmd.Name, cmd.Old)
			if rbErr := gitRepo.Storer.SetReference(ref); rbErr != nil {
				slog.Error("branch protection rollback failed", "ref", cmd.Name.String(), "error", rbErr)
			}
			http.Error(w, "push rejected: "+err.Error(), http.StatusForbidden)
			return
		}
	}

	// Dispatch push webhooks for each updated branch
	pusherName := gu.Username
	for _, cmd := range req.Commands {
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

	commands := req.Commands
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

// isForcePushHTTP returns true when the push is non-fast-forward (old commit is not an ancestor of new).
func isForcePushHTTP(gitRepo *gogit.Repository, cmd *packp.Command) bool {
	oldCommit, err := gitRepo.CommitObject(cmd.Old)
	if err != nil {
		return false
	}
	newCommit, err := gitRepo.CommitObject(cmd.New)
	if err != nil {
		return false
	}
	isAncestor, err := oldCommit.IsAncestor(newCommit)
	if err != nil {
		return false
	}
	return !isAncestor
}
