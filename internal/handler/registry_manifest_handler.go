package handler

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// registryManifestOp serves the manifest operations and reports false for any other.
func (h *Handler) registryManifestOp(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) bool {
	switch {
	case route.Op == service.RegistryOpManifest && r.Method == http.MethodPut:
		h.registryPutManifest(w, r, route)
	case route.Op == service.RegistryOpManifest && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.registryGetManifest(w, r, route)
	case route.Op == service.RegistryOpManifest && r.Method == http.MethodDelete:
		h.registryDelete(w, r, func() error {
			return h.Services.Registry.DeleteManifest(r.Context(), registryUserID(r), route.Name, route.Ref)
		})
	case route.Op == service.RegistryOpBlob && r.Method == http.MethodDelete:
		h.registryDelete(w, r, func() error {
			return h.Services.Registry.DeleteBlob(r.Context(), registryUserID(r), route.Name, route.Ref)
		})
	case route.Op == service.RegistryOpTags && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.registryListTags(w, r, route)
	default:
		return false
	}
	return true
}

func (h *Handler) registryDelete(w http.ResponseWriter, _ *http.Request, do func() error) {
	if err := do(); err != nil {
		writeRegistryFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) registryListTags(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) {
	limit := -1
	if raw := r.URL.Query().Get("n"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			middleware.WriteRegistryError(w, http.StatusBadRequest, "PAGINATION_NUMBER_INVALID", "n must be a non-negative integer", nil)
			return
		}
		limit = n
	}
	last := r.URL.Query().Get("last")
	tags, more, err := h.Services.Registry.ListTags(r.Context(), registryUserID(r), route.Name, last, limit)
	if err != nil {
		writeRegistryFailure(w, err)
		return
	}
	if more {
		next := url.Values{"last": {tags[len(tags)-1]}, "n": {strconv.Itoa(limit)}}
		w.Header().Set("Link", "</v2/"+route.Name+"/tags/list?"+next.Encode()+`>; rel="next"`)
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	writeJSON(w, http.StatusOK, map[string]any{"name": route.Name, "tags": tags})
}

func (h *Handler) registryPutManifest(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) {
	digest, err := h.Services.Registry.PutManifest(r.Context(), registryUserID(r), route.Name, route.Ref, r.Header.Get("Content-Type"), r.Body)
	if err != nil {
		writeRegistryFailure(w, err)
		return
	}
	w.Header().Set("Location", "/v2/"+route.Name+"/manifests/"+digest)
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) registryGetManifest(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) {
	m, err := h.Services.Registry.GetManifest(r.Context(), registryUserID(r), route.Name, route.Ref, r.Header.Values("Accept"))
	if err != nil {
		writeRegistryFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", m.MediaType)
	w.Header().Set("Docker-Content-Digest", m.Digest)
	w.Header().Set("ETag", `"`+m.Digest+`"`)
	// Read access is rechecked on every request, so no shared cache may keep a manifest after a Repository goes private.
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(m.Content)))
	if r.Method == http.MethodHead {
		return
	}
	h.Services.Registry.CountPull(m)
	_, _ = w.Write(m.Content)
}
