package handler

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/mkappworks/cloudzilla/internal/middleware"
)

func (h *Handler) GitInfoRefs(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := strings.TrimSuffix(chi.URLParam(r, "repo"), ".git")

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

	// Check permissions — receive-pack needs write access
	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}

	if svc == "git-receive-pack" {
		if userID == nil || !h.Services.Repo.CanWrite(r.Context(), repo, *userID) {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "access denied", http.StatusUnauthorized)
			return
		}
	} else {
		if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
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

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
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

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
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

	// Dispatch push webhooks for each updated branch
	for _, cmd := range req.Commands {
		if !strings.HasPrefix(cmd.Name.String(), "refs/heads/") {
			continue
		}
		if cmd.Action() == packp.Delete {
			continue
		}
		branch := strings.TrimPrefix(cmd.Name.String(), "refs/heads/")
		go h.Services.Webhook.Dispatch(repo.ID, "push",
			h.Services.Webhook.PushPayload(*repo, claims.Username, branch, cmd.New.String()))
	}
}
