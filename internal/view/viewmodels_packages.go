package view

import (
	"net/url"
	"sort"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// PackageCounts are the type tabs' badges, counted after the name filter.
type PackageCounts struct{ All, Container, Generic int }

// FilterPackages applies the name filter q (case-insensitive substring) and then the type tab; an unknown type is
// All.
func FilterPackages(all []model.PackageSummary, typ, q string) ([]model.PackageSummary, PackageCounts) {
	q = strings.ToLower(strings.TrimSpace(q))
	var counts PackageCounts
	shown := make([]model.PackageSummary, 0, len(all))
	for _, p := range all {
		if q != "" && !strings.Contains(strings.ToLower(p.Name), q) {
			continue
		}
		counts.All++
		switch p.Type {
		case model.PackageTypeContainer:
			counts.Container++
		case model.PackageTypeGeneric:
			counts.Generic++
		}
		if typ == model.PackageTypeContainer || typ == model.PackageTypeGeneric {
			if p.Type != typ {
				continue
			}
		}
		shown = append(shown, p)
	}
	return shown, counts
}

// RegistryHost is the host docker addresses the registry by: the host of server.base_url.
func RegistryHost(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// PullCommand pulls the newest tag, else the first, else the first untagged manifest by digest.
func PullCommand(host, owner, name string, tags []model.TagRow, untagged []model.ManifestRow, newest string) string {
	image := host + "/" + owner + "/" + name
	switch {
	case len(tags) > 0:
		tag := tags[0].Name
		for _, t := range tags {
			if t.Name == newest {
				tag = t.Name
			}
		}
		return "docker pull " + image + ":" + tag
	case len(untagged) > 0:
		return "docker pull " + image + "@" + untagged[0].Digest
	}
	return "docker pull " + image
}

// PackagesData is the repository Packages tab.
type PackagesData struct {
	BasePage
	Repo     model.Repository
	Packages []model.PackageSummary
	Counts   PackageCounts
	Type     string
	Query    string
	CanWrite bool
	// Empty is true when the repository has no package at all, not merely none matching the filter.
	Empty bool
	Host  string
	// BaseURL is server.base_url, for the curl how-to.
	BaseURL string
}

// PackageDetailData is a package's page. Pkg.OwnerName is the path owner, which differs from Repo.OwnerName after a
// transfer.
type PackageDetailData struct {
	BasePage
	Repo model.Repository
	Pkg  model.PackageSummary
	Host string
	// Mismatch is true when the path owner no longer owns the linked Repository.
	Mismatch bool
	// CanWrite shows tag and version delete; CanDelete shows Delete package and Re-link.
	CanWrite  bool
	CanDelete bool
	Command   string
	Tags      []model.TagRow
	Untagged  []model.ManifestRow
	Versions  []model.GenericVersionFiles
	// Platforms are those of the tag the pull command names.
	Platforms     []string
	ReadmeHTML    string
	ReadmeName    string
	Used, Limit   int64
	RelinkTargets []string
	Error         string
}

// OwnerPackagesData is the Packages tab of a user or organization profile.
type OwnerPackagesData struct {
	Owner string
	// Packages are those matching the filters; Total counts all the viewer sees under the path.
	Packages    []model.PackageSummary
	Total       int
	Type, Repo  string
	RepoOptions []string
	// CanManage is the path owner: Delete and Re-link for every package, whichever Repository it sits in.
	CanManage bool
	Targets   []string
}

// FilterOwnerPackages applies the type tab and the Repository filter; an empty or unknown value matches all.
func FilterOwnerPackages(all []model.PackageSummary, typ, repo string) []model.PackageSummary {
	shown := make([]model.PackageSummary, 0, len(all))
	for _, p := range all {
		if (typ == model.PackageTypeContainer || typ == model.PackageTypeGeneric) && p.Type != typ {
			continue
		}
		if repo != "" && p.RepoName != repo {
			continue
		}
		shown = append(shown, p)
	}
	return shown
}

// OwnerPackageRepos names the Repositories the packages are linked to, sorted, for the filter.
func OwnerPackageRepos(all []model.PackageSummary) []string {
	seen := map[string]bool{}
	var names []string
	for _, p := range all {
		if !seen[p.RepoName] {
			seen[p.RepoName] = true
			names = append(names, p.RepoName)
		}
	}
	sort.Strings(names)
	return names
}
