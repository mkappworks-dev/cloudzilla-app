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

	"github.com/go-chi/chi/v5"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/service"
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
			go h.Services.AccessToken.UpdateLastUsed(context.Background(), token.ID)
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

	srv := server.NewServer(server.MapLoader{"/": gitRepo.Storer})

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-advertisement", strings.TrimPrefix(svc, "git-")))

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

	srv := server.NewServer(server.MapLoader{"/": gitRepo.Storer})
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

	srv := server.NewServer(server.MapLoader{"/": gitRepo.Storer})
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

	status, err := sess.ReceivePack(r.Context(), req)
	if err != nil {
		http.Error(w, "receive-pack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

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
		go h.Services.Webhook.Dispatch(repo.ID, "push",
			h.Services.Webhook.PushPayload(*repo, pusherName, branch, cmd.New.String()))
	}

	// Re-index the repository for code search after each push.
	go func() {
		_ = h.Services.Index.IndexRepo(context.Background(), repo)
	}()

	// Re-parse dependency manifests for code graph after each push.
	go func() {
		if err := h.Services.Dependency.ParseAndStore(context.Background(), repo); err != nil {
			slog.Error("dependency: failed to parse and store manifests",
				"repo_id", repo.ID,
				"owner", repo.OwnerName,
				"repo", repo.Name,
				"error", err,
			)
		}
	}()
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
