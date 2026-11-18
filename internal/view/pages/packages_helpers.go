package pages

import (
	"net/url"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

func packageTypeLabel(typ string) string {
	if typ == model.PackageTypeContainer {
		return "Container"
	}
	return "Generic"
}

func packageHref(repo model.Repository, p model.PackageSummary) string {
	name := p.Name
	if p.Type == model.PackageTypeGeneric {
		name = url.PathEscape(name)
	}
	return "/" + repo.OwnerName + "/" + repo.Name + "/packages/" + p.Type + "/" + name
}

func packageTabHref(data view.PackagesData, typ string) string {
	q := url.Values{}
	if typ != "" {
		q.Set("type", typ)
	}
	if data.Query != "" {
		q.Set("q", data.Query)
	}
	href := "/" + data.Repo.OwnerName + "/" + data.Repo.Name + "/packages"
	if len(q) > 0 {
		href += "?" + q.Encode()
	}
	return href
}

func packageUsername(data view.PackagesData) string {
	if data.CurrentUser != nil {
		return data.CurrentUser.Username
	}
	return "<username>"
}

func packageDockerHowTo(data view.PackagesData) string {
	return "docker login " + data.Host + " -u " + packageUsername(data) + "\n" +
		"docker tag myapp " + data.Host + "/" + data.Repo.OwnerName + "/myapp:1.0.0\n" +
		"docker push " + data.Host + "/" + data.Repo.OwnerName + "/myapp:1.0.0"
}

func packageCurlHowTo(data view.PackagesData) string {
	return "curl -X PUT -H \"Authorization: Bearer $CZ_TOKEN\" \\\n" +
		"  --upload-file dist/app.tar.gz \\\n" +
		"  " + data.BaseURL + "/api/repos/" + data.Repo.OwnerName + "/" + data.Repo.Name + "/packages/generic/app/1.0.0/app.tar.gz"
}

// packageFormPath is where the Packages-page forms of the package's linked Repository post: delete-tag,
// delete-version, delete and relink.
func packageFormPath(data view.PackageDetailData, action string) string {
	path := map[string]string{"delete-tag": "tags/delete", "delete-version": "versions/delete", "delete": "delete", "relink": "relink"}[action]
	return "/" + data.Repo.OwnerName + "/" + data.Repo.Name + "/packages/" + path
}

func packageFileHref(data view.PackageDetailData, version, filename string) string {
	return "/" + data.Repo.OwnerName + "/" + data.Repo.Name + "/packages/generic/" +
		url.PathEscape(data.Pkg.Name) + "/" + url.PathEscape(version) + "/" + url.PathEscape(filename)
}

func packageFilesSize(files []model.PackageFile) int64 {
	var n int64
	for _, f := range files {
		n += f.Size
	}
	return n
}
