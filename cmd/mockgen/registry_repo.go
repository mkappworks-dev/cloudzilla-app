package main

import (
	"github.com/a-h/templ"

	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func repoPages() []page {
	repoView := func(can bool, edit func(*view.RepoData)) func(view.BasePage) templ.Component {
		return func(b view.BasePage) templ.Component {
			d := atlasRepoData(b, can)
			if edit != nil {
				edit(&d)
			}
			return pages.Repo(d)
		}
	}
	emptyRepo := func(d *view.RepoData) {
		d.Entries, d.AllFiles, d.ReadmeHTML, d.LatestCommit = nil, nil, "", view.TreeLatestCommit{}
		d.Branches, d.Tags, d.BranchCount, d.TagCount, d.CommitCount = nil, nil, 0, 0, 0
		d.Releases, d.TopContribs, d.Languages = nil, nil, nil
	}
	fileForm := func(b view.BasePage) view.BasePage { return repoChrome(b, atlasRepo(), true) }

	return []page{
		{
			Name: "repo", Route: "/{owner}/{repo}", Templ: "repo.templ", Title: "Repository",
			Variants: []variant{
				{Note: "writer on a repository with a README", Viewer: Writer, Render: repoView(true, nil)},
				{Name: "viewer", Note: "signed in without write access: no Add file, no edit", Viewer: Reader, Render: repoView(false, nil)},
				{Name: "anon", Note: "signed out: Star and Fork lead to sign-in", Viewer: Anon, Render: repoView(false, nil)},
				{Name: "empty", Note: "no commits yet", Viewer: Writer, Render: repoView(true, emptyRepo)},
				{Name: "empty-viewer", Note: "no commits yet, without write access", Viewer: Reader, Render: repoView(false, emptyRepo)},
				{Name: "archived", Note: "archived banner, read-only", Viewer: Writer, Render: repoView(true, func(d *view.RepoData) {
					d.IsArchived = true
					d.Repo.IsArchived = true
				})},
				{Name: "mirror", Note: "pull mirror with a sync banner", Viewer: Writer, Render: repoView(true, func(d *view.RepoData) {
					d.Repo.IsMirror = true
					d.Mirror = &view.MirrorBanner{
						RemoteURL: "https://git.example.test/geo/hollow-atlas", RemoteLabel: "git.example.test/geo/hollow-atlas",
						SyncedAgo: "2 hours ago", SyncURL: atlasPath + "/mirror/sync",
					}
				})},
				{Name: "mirror-failed", Note: "pull mirror whose last sync failed", Viewer: Writer, Render: repoView(true, func(d *view.RepoData) {
					d.Repo.IsMirror = true
					d.Mirror = &view.MirrorBanner{
						RemoteURL: "https://git.example.test/geo/hollow-atlas", RemoteLabel: "git.example.test/geo/hollow-atlas",
						SyncedAgo: "1 hour ago", Failed: true, Error: "Authentication failed for the remote.", NextTry: "in 30 minutes",
						SyncURL: atlasPath + "/mirror/sync", SettingsURL: atlasPath + "/settings",
					}
				})},
				{Name: "fork", Note: "a fork shows its source", Viewer: Writer, Render: repoView(true, func(d *view.RepoData) {
					d.IsFork, d.ForkOfPath = true, "ada-silva/hollow-atlas"
				})},
			},
		},
		{
			Name: "tree", Route: "/{owner}/{repo}/tree/{ref}/{path}", Templ: "tree.templ", Title: "Directory",
			Variants: []variant{
				{Note: "a directory with the file tree open", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Tree(atlasTreeData(b, true))
				}},
				{Name: "empty", Note: "an empty directory", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := atlasTreeData(b, true)
					d.Entries, d.LatestCommit = nil, view.TreeLatestCommit{}
					return pages.Tree(d)
				}},
			},
		},
		{
			Name: "blob", Route: "/{owner}/{repo}/blob/{ref}/{path}", Templ: "blob.templ", Title: "File",
			Variants: []variant{
				{Note: "writer: Edit and Delete", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Blob(atlasBlobData(b, true))
				}},
				{Name: "viewer", Note: "read-only: no Edit or Delete", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.Blob(atlasBlobData(b, false))
				}},
				{Name: "binary", Note: "a binary file offers only the raw download", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := atlasBlobData(b, true)
					d.Path, d.IsBinary, d.Lines, d.Size, d.CanEdit = "docs/architecture.png", true, nil, 48213, false
					d.Breadcrumbs = breadcrumbs("docs", "architecture.png")
					return pages.Blob(d)
				}},
			},
		},
		{
			Name: "blame", Route: "/{owner}/{repo}/blame/{ref}/{path}", Templ: "blame.templ", Title: "Blame",
			Variants: []variant{
				{Note: "per-line history with runs of one commit", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Blame(atlasBlameData(b, true))
				}},
			},
		},
		{
			Name: "edit_file", Route: "/{owner}/{repo}/edit/{ref}/{path}", Templ: "edit_file.templ", Title: "Edit file",
			Variants: []variant{
				{Note: "editing an existing file", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.EditFile(view.EditFileData{
						BasePage: fileForm(b), Owner: fixtureUser, RepoName: atlasName, Ref: "main", Path: blobPath,
						BlobSHA: "b7c1e0a94d2f35861a0c9e7d4b3f2a1c0d9e8f77", NewPath: blobPath, Content: blobSource,
					})
				}},
				{Name: "conflict", Note: "the file changed after the page loaded", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.EditFile(view.EditFileData{
						BasePage: fileForm(b), Owner: fixtureUser, RepoName: atlasName, Ref: "main", Path: blobPath,
						BlobSHA: "b7c1e0a94d2f35861a0c9e7d4b3f2a1c0d9e8f77", NewPath: blobPath, Content: blobSource,
						Message: "Tidy the index", Error: "This file changed while you were editing it.",
						ConflictURL: codeurl.Path(fixtureUser, atlasName, "blob", "main", blobPath),
					})
				}},
			},
		},
		{
			Name: "new_file", Route: "/{owner}/{repo}/new/{ref}", Templ: "new_file.templ", Title: "New file",
			Variants: []variant{
				{Note: "creating a file in the repository root", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.NewFile(view.NewFileData{BasePage: fileForm(b), Owner: fixtureUser, RepoName: atlasName, Ref: "main"})
				}},
				{Name: "error", Note: "a refused path with the upload to choose again", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.NewFile(view.NewFileData{
						BasePage: fileForm(b), Owner: fixtureUser, RepoName: atlasName, Ref: "main", Dir: "docs", Path: "docs/.git/config",
						Message: "Add config", Error: "That path is not allowed.", UploadName: "config.toml",
					})
				}},
			},
		},
		{
			Name: "commits", Route: "/{owner}/{repo}/commits/{ref}", Templ: "commits.templ", Title: "Commits",
			Variants: []variant{
				{Note: "a middle page, with newer and older links", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Commits(view.CommitsData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
						RefsURL: atlasPath + "/refs",
						Log:     &service.CommitLog{Commits: atlasCommitSummaries(), Ref: "main", Page: 2, PrevPage: 1, NextPage: 3, HasMore: true},
					})
				}},
				{Name: "empty", Note: "no commits on this ref", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Commits(view.CommitsData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
						RefsURL: atlasPath + "/refs", Log: &service.CommitLog{Ref: "main", Page: 1},
					})
				}},
			},
		},
		{
			Name: "commit", Route: "/{owner}/{repo}/commit/{sha}", Templ: "commit.templ", Title: "Commit",
			Variants: []variant{
				{Note: "a commit with checks, an edit, a new file and a binary", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Commit(view.CommitData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
						Commit: atlasCommitDetail(), Statuses: atlasStatuses(),
					})
				}},
				{Name: "no-checks", Note: "no commit statuses", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Commit(view.CommitData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
						Commit: atlasCommitDetail(),
					})
				}},
			},
		},
		{
			Name: "refs", Route: "/{owner}/{repo}/refs", Templ: "refs.templ", Title: "Branches and tags",
			Variants: []variant{
				{Note: "writer: create and delete branches and tags", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Refs(view.RefsData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
						Branches: atlasBranches(), Tags: atlasTags(), CanWrite: true,
					})
				}},
				{Name: "viewer", Note: "read-only lists", Viewer: Reader, Render: func(b view.BasePage) templ.Component {
					return pages.Refs(view.RefsData{
						BasePage: repoChrome(b, atlasRepo(), false), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
						Branches: atlasBranches(), Tags: atlasTags(),
					})
				}},
				{Name: "empty", Note: "no branches or tags", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Refs(view.RefsData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName, CanWrite: true,
					})
				}},
			},
		},
		{
			Name: "contributors", Route: "/{owner}/{repo}/graphs/contributors", Templ: "contributors.templ", Title: "Contributors",
			Variants: []variant{
				{Note: "commit counts with a weekly sparkline", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					r := atlasRepo()
					return pages.Contributors(view.ContributorsData{BasePage: repoChrome(b, r, true), Repo: &r, Rows: contributorRows()})
				}},
				{Name: "empty", Note: "no commits yet", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					r := atlasRepo()
					return pages.Contributors(view.ContributorsData{BasePage: repoChrome(b, r, true), Repo: &r})
				}},
			},
		},
		{
			Name: "pulse", Route: "/{owner}/{repo}/pulse", Templ: "pulse.templ", Title: "Pulse",
			Variants: []variant{
				{Note: "30 days of issues, pull requests and commits", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Pulse(view.PulseData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
						IssuesOpened: 6, IssuesClosed: 4, PullsOpened: 5, PullsMerged: 3, RecentCommits: 41,
						CommitsLast30: last30(3), IssuesLast30: last30(2), PullsLast30: last30(5), Contributors: contributorRows(),
					})
				}},
				{Name: "empty", Note: "no activity", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Pulse(view.PulseData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
						CommitsLast30: make([]int, 30), IssuesLast30: make([]int, 30), PullsLast30: make([]int, 30),
					})
				}},
			},
		},
		{
			Name: "dependencies", Route: "/{owner}/{repo}/network/dependencies", Templ: "dependencies.templ", Title: "Dependencies",
			Variants: []variant{
				{Note: "Go and npm manifests", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Dependencies(view.DependenciesData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
						Groups: dependencyGroups(),
					})
				}},
				{Name: "empty", Note: "no manifest found", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Dependencies(view.DependenciesData{
						BasePage: repoChrome(b, atlasRepo(), true), Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName,
					})
				}},
			},
		},
		{
			Name: "stargazers", Route: "/{owner}/{repo}/stargazers", Templ: "stargazers.templ", Title: "Stargazers",
			Variants: []variant{
				{Note: "people who starred", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Stargazers(view.StargazersData{
						BasePage: b, Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName, Stargazers: stargazerUsers(), StarCount: 3,
					})
				}},
				{Name: "empty", Note: "no stars yet", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Stargazers(view.StargazersData{BasePage: b, Repo: atlasRepo(), Owner: fixtureUser, RepoName: atlasName})
				}},
			},
		},
		{
			Name: "activity", Route: "/activity", Templ: "activity.templ", Title: "Activity feed",
			Variants: []variant{
				{Note: "every kind of event, more pages to come", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Activity(activityData(b, "all", atlasEvents(), false))
				}},
				{Name: "empty", Note: "no activity in this scope", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Activity(activityData(b, "watching", nil, false))
				}},
				{Name: "error", Note: "the feed query failed", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Activity(activityData(b, "all", nil, true))
				}},
			},
		},
		{
			Name: "code_search", Route: "/search/code", Templ: "code_search.templ", Title: "Code search",
			Variants: []variant{
				{Note: "results with the match highlighted", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.CodeSearch(view.CodeSearchData{
						BasePage: b, Query: "TileIndex", Lang: "go", Results: codeSearchResults(), Total: 45, Page: 2,
					})
				}},
				{Name: "empty", Note: "no results", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.CodeSearch(view.CodeSearchData{BasePage: b, Query: "zzqxv", RepoFilter: "mkappworks/hollow-atlas", Page: 1})
				}},
				{Name: "no-query", Note: "before a search", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.CodeSearch(view.CodeSearchData{BasePage: b})
				}},
			},
		},
		{
			Name: "search", Route: "/search", Templ: "search.templ", Title: "Search",
			Variants: []variant{
				{Note: "results of every kind", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Search(view.SearchData{BasePage: b, Query: "atlas", Type: "all", Results: searchResults("atlas")})
				}},
				{Name: "repos", Note: "repositories tab only", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					r := searchResults("atlas")
					r.Type = "repos"
					return pages.Search(view.SearchData{BasePage: b, Query: "atlas", Type: "repos", Results: r})
				}},
				{Name: "empty", Note: "no results", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Search(view.SearchData{BasePage: b, Query: "zzqxv", Type: "all", Results: &service.SearchResults{Query: "zzqxv", Type: "all"}})
				}},
				{Name: "no-query", Note: "before a search", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Search(view.SearchData{BasePage: b})
				}},
				{Name: "anon", Note: "signed out", Viewer: Anon, Render: func(b view.BasePage) templ.Component {
					return pages.Search(view.SearchData{BasePage: b, Query: "atlas", Type: "all", Results: searchResults("atlas")})
				}},
			},
		},
		{
			Name: "topic", Route: "/topic/{name}", Templ: "topic.templ", Title: "Topic",
			Variants: []variant{
				{Note: "repositories under a topic, sorted by stars", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Topic(view.TopicData{BasePage: b, TopicName: "maps", Repos: topicRepos(), Total: 23, Sort: "stars", Page: 2, HasNext: true})
				}},
				{Name: "empty", Note: "no repositories", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.Topic(view.TopicData{BasePage: b, TopicName: "cartography", Sort: "stars", Page: 1})
				}},
			},
		},
		{
			Name: "repo_fork", Route: "/{owner}/{repo}/fork", Templ: "repo_fork.templ", Title: "Fork",
			Variants: []variant{
				{Note: "choose where to fork", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.RepoFork(forkData(b, []string{fixtureUser, "paper-planes"}, nil))
				}},
				{Name: "existing", Note: "the viewer already has a fork", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					return pages.RepoFork(forkData(b, []string{"paper-planes"},
						[]view.RepoRef{{Name: fixtureUser + "/geo-cache", Path: "/" + fixtureUser + "/geo-cache"}}))
				}},
				{Name: "no-owner", Note: "no namespace to fork into", Viewer: Writer, Render: func(b view.BasePage) templ.Component {
					d := forkData(b, nil, nil)
					d.SourceOwner = fixtureUser
					return pages.RepoFork(d)
				}},
			},
		},
	}
}

func dependencyGroups() []components.DependencyGroupData {
	return []components.DependencyGroupData{
		{PackageMgr: "go.mod", Label: "Go modules", Rows: []components.DependencyRow{
			{Package: "github.com/go-chi/chi/v5", Version: "v5.1.0"}, {Package: "github.com/jmoiron/sqlx", Version: "v1.4.0"},
			{Package: "github.com/spf13/cobra", Version: "v1.8.1"},
		}},
		{PackageMgr: "package.json", Label: "npm", Rows: []components.DependencyRow{
			{Package: "maplibre-gl", Version: "^4.5.0"}, {Package: "typescript", Version: "^5.5.4", IsDev: true},
		}},
	}
}

func activityData(b view.BasePage, filter string, events []model.Event, failed bool) view.ActivityData {
	b = onAccountPage(b, "activity")
	d := view.ActivityData{
		BasePage: b, Username: fixtureUser, Events: events, LoadFailed: failed, Filter: filter, Page: 1,
		ScopeCounts: map[string]int{"all": 42, "yours": 17, "watching": 25},
	}
	if len(events) > 0 {
		d.Page, d.HasMore, d.PrevURL, d.NextURL = 2, true, "/activity", "/activity?page=3"
	}
	return d
}

func forkData(b view.BasePage, owners []string, existing []view.RepoRef) view.RepoForkData {
	return view.RepoForkData{
		BasePage: b, SourceOwner: "ada-silva", SourceName: "geo-cache", Description: "Tile cache for offline maps.",
		DefaultBranch: "main", Owners: owners, ExistingForks: existing,
	}
}
