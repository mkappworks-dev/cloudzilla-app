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

	// Check permissions
	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}

	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	service := r.URL.Query().Get("service")
	if service != "git-upload-pack" && service != "git-receive-pack" {
		http.Error(w, "invalid service", http.StatusBadRequest)
		return
	}

	repoPath := filepath.Join(h.Cfg.Git.ReposRoot, owner, repoName+".git")
	gitRepo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		http.Error(w, "failed to open repository", http.StatusInternalServerError)
		return
	}

	storer := gitRepo.Storer
	ep, err := transport.NewEndpoint("/")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	srv := server.NewServer(server.MapLoader{"/": storer})

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-git-%s-advertisement", strings.TrimPrefix(service, "git-")))

	if service == "git-upload-pack" {
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

		if err := ar.Encode(w); err != nil {
			return
		}
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

		if err := ar.Encode(w); err != nil {
			return
		}
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

	// Check permissions
	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}

	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	repoPath := filepath.Join(h.Cfg.Git.ReposRoot, owner, repoName+".git")
	_, err = gogit.PlainOpen(repoPath)
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

	// Read request body
	_, err = io.ReadAll(body)
	if err != nil {
		http.Error(w, "failed to read request", http.StatusBadRequest)
		return
	}

	// This is a simplified implementation
	// For production, you'd need full pack protocol support
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) GitReceivePack(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := strings.TrimSuffix(chi.URLParam(r, "repo"), ".git")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	// Check permissions - need write access
	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}

	if userID == nil || !h.Services.Repo.CanWrite(r.Context(), repo, *userID) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	repoPath := filepath.Join(h.Cfg.Git.ReposRoot, owner, repoName+".git")
	_, err = gogit.PlainOpen(repoPath)
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

	// Read request body
	_, err = io.ReadAll(body)
	if err != nil {
		http.Error(w, "failed to read request", http.StatusBadRequest)
		return
	}

	// This is a simplified implementation
	// For production, you'd need full pack protocol support
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	w.WriteHeader(http.StatusOK)
}
