package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// packageURL is a package's page under the Repository it is linked to.
func packageURL(repo *model.Repository, typ, name string) string {
	if typ == model.PackageTypeGeneric {
		name = url.PathEscape(name)
	}
	return "/" + repo.OwnerName + "/" + repo.Name + "/packages/" + typ + "/" + name
}

// PagePackages renders GET /{owner}/{repo}/packages.
func (h *Handler) PagePackages(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.readableRepoPage(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if !ok {
		return
	}
	all, err := h.Services.Package.ListByRepo(r.Context(), repo, viewerOf(r))
	if err != nil {
		slog.Error("packages page: list failed", "repo_id", repo.ID, "error", err)
		h.ServerError(w, r)
		return
	}
	typ, q := r.URL.Query().Get("type"), r.URL.Query().Get("q")
	shown, counts := view.FilterPackages(all, typ, q)
	canWrite := false
	if id := viewerOf(r); id != nil {
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, *id)
	}
	h.render(w, r, pages.Packages(view.PackagesData{
		CanWrite: canWrite,
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "packages", h.canManageRepo(r, repo)),
		Repo:     *repo,
		Packages: shown,
		Counts:   counts,
		Type:     typ,
		Query:    q,
		Empty:    len(all) == 0,
		Host:     view.RegistryHost(h.Cfg.Server.BaseURL),
		BaseURL:  strings.TrimRight(h.Cfg.Server.BaseURL, "/"),
	}))
}

// PagePackageContainer renders GET /{owner}/{repo}/packages/container/{name...}; a name may hold slashes.
func (h *Handler) PagePackageContainer(w http.ResponseWriter, r *http.Request) {
	h.packageDetailPage(w, r, model.PackageTypeContainer, unescapeRouted(r, chi.URLParam(r, "*")))
}

// PagePackageGeneric renders GET /{owner}/{repo}/packages/generic/{name}.
func (h *Handler) PagePackageGeneric(w http.ResponseWriter, r *http.Request) {
	h.packageDetailPage(w, r, model.PackageTypeGeneric, unescapeRouted(r, chi.URLParam(r, "name")))
}

func (h *Handler) packageDetailPage(w http.ResponseWriter, r *http.Request, typ, name string) {
	repo, ok := h.readableRepoPage(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if !ok {
		return
	}
	ctx := r.Context()
	viewer := viewerOf(r)
	sum, err := h.Services.Package.InRepo(ctx, repo, viewer, typ, name)
	if errors.Is(err, service.ErrPackageNotFound) {
		h.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("package page: find failed", "repo_id", repo.ID, "error", err)
		h.ServerError(w, r)
		return
	}

	data := view.PackageDetailData{
		Repo:     *repo,
		Pkg:      *sum,
		Host:     view.RegistryHost(h.Cfg.Server.BaseURL),
		Mismatch: !strings.EqualFold(sum.OwnerName, repo.OwnerName),
	}
	if viewer != nil {
		data.CanWrite = h.Services.Repo.CanWrite(ctx, repo, *viewer)
		data.CanDelete = h.Services.Package.CanDelete(ctx, &sum.Package, repo, *viewer)
	}
	switch typ {
	case model.PackageTypeContainer:
		d, err := h.Services.Package.ContainerDetail(ctx, sum.OwnerName, name, viewer)
		if err != nil {
			slog.Error("package page: container detail failed", "package_id", sum.ID, "error", err)
			h.ServerError(w, r)
			return
		}
		data.Tags, data.Untagged = d.Tags, d.Untagged
		data.Command = view.PullCommand(data.Host, sum.OwnerName, name, d.Tags, d.Untagged, sum.LatestVersion)
		for _, t := range d.Tags {
			if t.Name == sum.LatestVersion {
				data.Platforms = t.Platforms
			}
		}
	default:
		_, versions, err := h.Services.Package.GenericVersions(ctx, repo.ID, name)
		if err != nil {
			slog.Error("package page: generic versions failed", "package_id", sum.ID, "error", err)
			h.ServerError(w, r)
			return
		}
		data.Versions = versions
		if len(versions) > 0 && len(versions[0].Files) > 0 {
			f := versions[0].Files[0]
			data.Command = "curl -LO " + strings.TrimRight(h.Cfg.Server.BaseURL, "/") + "/" + repo.OwnerName + "/" + repo.Name +
				"/packages/generic/" + url.PathEscape(name) + "/" + url.PathEscape(versions[0].Version) + "/" + url.PathEscape(f.Filename)
		}
	}

	html, readmeName := h.Services.Code.RepoReadme(ctx, repo.OwnerName, repo.Name, repo.DefaultBranch)
	data.ReadmeHTML, data.ReadmeName = string(html), readmeName
	if data.Used, data.Limit, err = h.Services.Package.Storage(ctx, &sum.Package); err != nil {
		slog.Warn("package page: storage lookup failed; hiding it", "package_id", sum.ID, "error", err)
	}
	if data.CanDelete {
		data.RelinkTargets = h.relinkTargets(r, repo, sum)
	}

	publishers := make([]string, 0, len(data.Tags))
	for _, t := range data.Tags {
		publishers = append(publishers, t.Publisher)
	}
	data.BasePage = h.withRepoSubnav(ctx, basePage(r, h.Services), repo, "packages", h.canManageRepo(r, repo))
	h.render(w, h.withAvatars(r, append(publishers, sum.OwnerName)...), pages.PackageDetail(data))
}

// relinkTargets names the path owner's other Repositories the viewer can see; Relink refuses the ones they may not use.
func (h *Handler) relinkTargets(r *http.Request, current *model.Repository, p *model.PackageSummary) []string {
	repos, err := h.Services.Repo.ListByOwnerVisibleTo(r.Context(), p.OwnerName, viewerOf(r))
	if err != nil {
		slog.Warn("package page: re-link targets lookup failed", "package_id", p.ID, "error", err)
		return nil
	}
	var out []string
	for _, t := range repos {
		if t.ID != current.ID {
			out = append(out, t.Name)
		}
	}
	return out
}
