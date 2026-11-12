package main

import (
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
	"github.com/mkappworks-dev/cloudzilla-app/internal/highlight"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

const (
	atlasName = "hollow-atlas"
	atlasPath = "/" + fixtureUser + "/" + atlasName
	atlasHead = "3f9c2ab71d04e58c6b2a9f01d7e4c35a8b60f2de"
	blobPath  = "internal/service/tile_index.go"
)

const blobSource = `package service

import (
	"context"
	"fmt"
	"sync"
)

// TileIndex maps a tile key to the offset of its bytes in the pack file.
type TileIndex struct {
	mu      sync.RWMutex
	offsets map[string]int64
}

func NewTileIndex() *TileIndex {
	return &TileIndex{offsets: map[string]int64{}}
}

func (i *TileIndex) Lookup(ctx context.Context, key string) (int64, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	off, ok := i.offsets[key]
	if !ok {
		return 0, fmt.Errorf("tile %q: not in index", key)
	}
	return off, nil
}

func (i *TileIndex) Put(key string, off int64) {
	i.mu.Lock()
	i.offsets[key] = off
	i.mu.Unlock()
}
`

func atlasRepo() model.Repository {
	r := fixtureRepos()[0]
	r.Website = "https://hollow-atlas.example.test"
	r.License = "MIT"
	r.ForkCount = 3
	return r
}

// repoChrome adds what the handlers' withRepoSubnav adds around a repository page.
func repoChrome(b view.BasePage, repo model.Repository, canManage bool) view.BasePage {
	b.OwnerContext = repo.OwnerName
	b.RepoSubnav = &view.RepoSubnavInfo{
		OwnerName: repo.OwnerName, RepoName: repo.Name, Active: "code", CanManage: canManage, Private: repo.Private,
		AllowIssues: repo.AllowIssues, AllowDiscussions: repo.AllowDiscussions, AllowProjects: repo.AllowProjects,
		AllowWiki: repo.AllowWiki,
		Counts:    map[string]int{"pull_requests": 2, "issues": 5, "discussions": 3, "releases": 2},
	}
	for _, r := range fixtureRepos() {
		b.RepoSwitcher = append(b.RepoSwitcher, view.RepoRef{Name: r.Name, Path: "/" + r.OwnerName + "/" + r.Name})
	}
	return b
}

func atlasBranches() []service.BranchInfo {
	return []service.BranchInfo{
		{Name: "main", Hash: atlasHead, IsDefault: true},
		{Name: "feat/tile-cache", Hash: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"},
		{Name: "fix/index-race", Hash: "0f1e2d3c4b5a69788796a5b4c3d2e1f001122334"},
	}
}

func atlasTags() []service.TagInfo {
	return []service.TagInfo{
		{Name: "v0.4.0", Hash: "9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a291807"},
		{Name: "v0.3.2", Hash: "5d4c3b2a1908f7e6d5c4b3a2918f7e6d5c4b3a29"},
	}
}

func atlasLatestCommit() view.TreeLatestCommit {
	return view.TreeLatestCommit{
		SHA: atlasHead[:7], Message: "Cache tile index on disk (#41)", Author: "ada-silva", AuthorURL: "/ada-silva",
		CommitURL: atlasPath + "/commit/" + atlasHead, Timestamp: ago(days(9)),
	}
}

func treeEntry(name, path string, dir bool, msg string, age time.Duration) service.TreeEntryWithLastCommit {
	e := service.TreeEntryWithLastCommit{Name: name, Path: path, IsDir: dir}
	e.LastCommit.SHA = atlasHead[:7]
	e.LastCommit.Message = msg
	e.LastCommit.Author = "ada-silva"
	e.LastCommit.Timestamp = ago(age)
	return e
}

func rootEntries() []service.TreeEntryWithLastCommit {
	return []service.TreeEntryWithLastCommit{
		treeEntry("cmd", "cmd", true, "Split the server and the packer into two binaries", days(40)),
		treeEntry("docs", "docs", true, "Document the pack file layout", days(22)),
		treeEntry("internal", "internal", true, "Cache tile index on disk (#41)", days(9)),
		treeEntry(".gitignore", ".gitignore", false, "Ignore local pack files", days(120)),
		treeEntry("Dockerfile", "Dockerfile", false, "Build a static binary in the image", days(60)),
		treeEntry("LICENSE", "LICENSE", false, "Add MIT license", days(190)),
		treeEntry("Makefile", "Makefile", false, "Add a release target", days(33)),
		treeEntry("README.md", "README.md", false, "Describe the offline sync flow", days(15)),
		treeEntry("go.mod", "go.mod", false, "Bump go to 1.24", days(28)),
		treeEntry("go.sum", "go.sum", false, "Bump go to 1.24", days(28)),
	}
}

func allFiles() []string {
	return []string{
		".gitignore", "Dockerfile", "LICENSE", "Makefile", "README.md", "go.mod", "go.sum", "cmd/packer/main.go",
		"cmd/server/main.go", "docs/pack-format.md", "internal/service/tile_index.go", "internal/service/tile_index_test.go",
		"internal/store/pack.go",
	}
}

const atlasReadme = `<h1>hollow-atlas</h1>
<p>Offline-first map tiles for self-hosted fleets. A vehicle keeps a pack file of the tiles it needs and syncs the difference when it regains a connection.</p>
<h2>Quick start</h2>
<pre><code>make build
./bin/server --pack ./tiles.pack
</code></pre>
<ul>
<li>Tiles are addressed by <code>z/x/y</code> and stored once.</li>
<li>The index is rebuilt from the pack on start, or loaded from disk when it is fresh.</li>
</ul>
<h2>License</h2>
<p>MIT</p>`

func atlasRepoData(b view.BasePage, can bool) view.RepoData {
	repo := atlasRepo()
	b = repoChrome(b, repo, can)
	return view.RepoData{
		BasePage: b, Repo: repo, Owner: fixtureUser, RepoName: atlasName,
		CloneHTTP: "https://cloudzilla.example.test" + atlasPath + ".git",
		CloneSSH:  "git@cloudzilla.example.test:" + fixtureUser + "/" + atlasName + ".git",
		CanWrite:  can, CanManage: can, ReadmeHTML: atlasReadme, ReadmeName: "README.md",
		StarCount: 128, ForkCount: repo.ForkCount, WatchLevel: "participating",
		Topics: []model.Topic{{ID: 1, Name: "maps"}, {ID: 2, Name: "offline-first"}},
		Languages: []components.LangBarItem{
			{Name: "Go", Percent: 78, Color: "#00ADD8"}, {Name: "TypeScript", Percent: 14, Color: "#3178C6"},
			{Name: "Makefile", Percent: 8, Color: "#427819"},
		},
		TopContribs: []service.ContributorStat{
			{Name: "ada-silva", Email: "ada@example.test", Commits: 112}, {Name: "mkappworks", Email: "mk@example.test", Commits: 64},
			{Name: "li-wei", Email: "li@example.test", Commits: 21},
		},
		Releases: []model.Release{
			{ID: 2, TagName: "v0.4.0", Name: "Offline index", PublishedAt: ptr(ago(days(30))), CreatedAt: ago(days(30))},
			{ID: 1, TagName: "v0.3.2", Name: "Fix pack rollover", PublishedAt: ptr(ago(days(80))), CreatedAt: ago(days(80))},
		},
		Entries: rootEntries(), LatestCommit: atlasLatestCommit(), Branches: atlasBranches(), Tags: atlasTags(),
		BranchCount: 3, TagCount: 2, CommitCount: 197, AllFiles: allFiles(),
	}
}

func ptr[T any](v T) *T { return &v }

func breadcrumbs(parts ...string) []service.BreadcrumbPart {
	out := []service.BreadcrumbPart{{Name: atlasName, URL: atlasPath}}
	path := ""
	for _, p := range parts {
		path = strings.TrimPrefix(path+"/"+p, "/")
		out = append(out, service.BreadcrumbPart{Name: p, URL: codeurl.Path(fixtureUser, atlasName, "tree", "main", path)})
	}
	return out
}

func sidebarNodes() []components.TreeNode {
	node := func(name, path string, dir bool) components.TreeNode {
		kind := "blob"
		if dir {
			kind = "tree"
		}
		return components.TreeNode{Name: name, IsDir: dir, Path: path, Href: codeurl.Path(fixtureUser, atlasName, kind, "main", path)}
	}
	service := node("service", "internal/service", true)
	service.IsOpen = true
	service.Children = []components.TreeNode{
		node("tile_index.go", blobPath, false), node("tile_index_test.go", "internal/service/tile_index_test.go", false),
	}
	service.Children[0].IsActive = true
	internal := node("internal", "internal", true)
	internal.IsOpen = true
	internal.Children = []components.TreeNode{service, node("store", "internal/store", true)}
	return []components.TreeNode{
		node("cmd", "cmd", true), node("docs", "docs", true), internal, node("Dockerfile", "Dockerfile", false),
		node("LICENSE", "LICENSE", false), node("Makefile", "Makefile", false), node("README.md", "README.md", false),
		node("go.mod", "go.mod", false),
	}
}

func atlasTreeData(b view.BasePage, can bool) view.TreeData {
	repo := atlasRepo()
	return view.TreeData{
		BasePage: repoChrome(b, repo, can), Repo: repo, Owner: fixtureUser, RepoName: atlasName, Ref: "main",
		Path: "internal/service", Breadcrumbs: breadcrumbs("internal", "service"),
		Entries: []service.TreeEntryWithLastCommit{
			treeEntry("tile_index.go", blobPath, false, "Cache tile index on disk (#41)", days(9)),
			treeEntry("tile_index_test.go", "internal/service/tile_index_test.go", false, "Cover index reload", days(9)),
		},
		RefsURL: atlasPath + "/refs", Branches: atlasBranches(), Tags: atlasTags(), CanManage: can, Sidebar: sidebarNodes(),
		ExpandAllURL: atlasPath + "/tree/main/internal/service?expand=1", LatestCommit: atlasLatestCommit(),
	}
}

func codeLines(filename, src string) []service.CodeLine {
	src = strings.TrimSuffix(src, "\n")
	html := highlight.Lines(filename, src)
	var out []service.CodeLine
	for i, text := range strings.Split(src, "\n") {
		l := service.CodeLine{Num: i + 1, Text: text}
		if i < len(html) {
			l.HTML = html[i]
		}
		out = append(out, l)
	}
	return out
}

func atlasBlobData(b view.BasePage, can bool) view.BlobData {
	repo := atlasRepo()
	lines := codeLines(blobPath, blobSource)
	return view.BlobData{
		BasePage: repoChrome(b, repo, can), Repo: repo, Owner: fixtureUser, RepoName: atlasName, Ref: "main", Path: blobPath,
		Breadcrumbs: breadcrumbs("internal", "service", "tile_index.go"), Branches: atlasBranches(), Tags: atlasTags(),
		Lines: lines, Size: int64(len(blobSource)),
		BlameURL:  codeurl.Path(fixtureUser, atlasName, "blame", "main", blobPath),
		RawURL:    codeurl.Path(fixtureUser, atlasName, "raw", "main", blobPath),
		EditURL:   codeurl.Path(fixtureUser, atlasName, "edit", "main", blobPath),
		DeleteURL: codeurl.Path(fixtureUser, atlasName, "delete", "main", blobPath), BlobSHA: "b7c1e0a94d2f35861a0c9e7d4b3f2a1c0d9e8f77",
		CanEdit: can, CanDelete: can, CanManage: can, Sidebar: sidebarNodes(),
		ExpandAllURL: atlasPath + "/blob/main/" + blobPath + "?expand=1", LatestCommit: atlasLatestCommit(),
	}
}

func atlasBlameData(b view.BasePage, can bool) view.BlameData {
	repo := atlasRepo()
	lines := codeLines(blobPath, blobSource)
	type run struct {
		upto   int
		hash   string
		author string
		age    time.Duration
	}
	runs := []run{
		{9, "7a1c4e2b9d03f5681a2b3c4d5e6f708192a3b4c5", "mkappworks", days(130)},
		{17, "3f9c2ab71d04e58c6b2a9f01d7e4c35a8b60f2de", "ada-silva", days(9)},
		{24, "c05d1e8f7a6b43928170fedcba9876543210ffee", "li-wei", days(61)},
		{99, "3f9c2ab71d04e58c6b2a9f01d7e4c35a8b60f2de", "ada-silva", days(9)},
	}
	var out []service.BlameLine
	r := 0
	for i, l := range lines {
		if i >= runs[r].upto && r < len(runs)-1 {
			r++
		}
		out = append(out, service.BlameLine{
			LineNum: l.Num, Text: l.Text, HTML: l.HTML, Hash: runs[r].hash[:7], FullHash: runs[r].hash,
			Author: runs[r].author, AuthorTime: ago(runs[r].age), ShowMeta: i == 0 || (r > 0 && i == runs[r-1].upto),
		})
	}
	return view.BlameData{
		BasePage: repoChrome(b, repo, can), Repo: repo, Owner: fixtureUser, RepoName: atlasName, Ref: "main", Path: blobPath,
		Breadcrumbs: breadcrumbs("internal", "service", "tile_index.go"), Branches: atlasBranches(), Tags: atlasTags(),
		Lines: out, BlobURL: codeurl.Path(fixtureUser, atlasName, "blob", "main", blobPath), Contributors: 3, CanManage: can,
	}
}

func atlasCommitSummaries() []service.CommitSummary {
	mk := func(hash, msg, author string, age time.Duration) service.CommitSummary {
		return service.CommitSummary{Hash: hash[:7], FullHash: hash, Message: msg, Author: author, AuthorTime: ago(age)}
	}
	return []service.CommitSummary{
		mk(atlasHead, "Cache tile index on disk (#41)", "ada-silva", days(9)),
		mk("e2d1c0b9a8f7e6d5c4b3a2918f7e6d5c4b3a2918", "Reload the index when the pack file changes", "ada-silva", days(9)+3*time.Hour),
		mk("d4c3b2a1908f7e6d5c4b3a2918f7e6d5c4b3a291", "Lock around index writes", "li-wei", days(10)),
		mk("c3b2a1908f7e6d5c4b3a2918f7e6d5c4b3a29180", "Fix tile key escaping", "mkappworks", days(12)),
		mk("b2a1908f7e6d5c4b3a2918f7e6d5c4b3a291807f", "Bump go to 1.24", "mkappworks", days(28)),
		mk("a1908f7e6d5c4b3a2918f7e6d5c4b3a291807f6e", "Add a release target", "mkappworks", days(33)),
	}
}

func atlasCommitDetail() *service.CommitDetail {
	line := func(file, typ, content string, oldNum, newNum int) service.DiffLine {
		l := service.DiffLine{Type: typ, Content: content, OldNum: oldNum, NewNum: newNum}
		if h := highlight.Lines(file, content); len(h) > 0 {
			l.HTML = h[0]
		}
		return l
	}
	const f = blobPath
	return &service.CommitDetail{
		Hash: atlasHead[:7], FullHash: atlasHead, Message: "Cache tile index on disk (#41)",
		Body:   "Rebuilding the index from the pack on every start took 40s on a fleet laptop.\nPersist it next to the pack and reload it when the pack is unchanged.",
		Author: "ada-silva", AuthorEmail: "ada@example.test", AuthorTime: ago(days(9)),
		ParentHashes: []string{"e2d1c0b9a8f7e6d5c4b3a2918f7e6d5c4b3a2918"},
		TotalAdded:   9, TotalDeleted: 2,
		Files: []service.FileDiff{
			{OldPath: f, NewPath: f, Added: 6, Deleted: 2, Hunks: []service.DiffHunk{{
				Header: "@@ -9,9 +9,13 @@ type TileIndex struct {",
				Lines: []service.DiffLine{
					line(f, "ctx", "type TileIndex struct {", 9, 9),
					line(f, "ctx", "\tmu      sync.RWMutex", 10, 10),
					line(f, "del", "\toffsets map[string]int64", 11, 0),
					line(f, "add", "\toffsets map[string]int64", 0, 11),
					line(f, "add", "\tpath    string", 0, 12),
					line(f, "ctx", "}", 12, 13),
					line(f, "ctx", "", 13, 14),
					line(f, "del", "func NewTileIndex() *TileIndex {", 14, 0),
					line(f, "add", "func NewTileIndex(path string) *TileIndex {", 0, 15),
				},
			}}},
			{OldPath: "/dev/null", NewPath: "internal/service/tile_index_disk.go", IsNew: true, Added: 3, Hunks: []service.DiffHunk{{
				Header: "@@ -0,0 +1,3 @@",
				Lines: []service.DiffLine{
					line("tile_index_disk.go", "add", "package service", 0, 1),
					line("tile_index_disk.go", "add", "", 0, 2),
					line("tile_index_disk.go", "add", "const indexFile = \"tiles.idx\"", 0, 3),
				},
			}}},
			{OldPath: "docs/index.png", NewPath: "docs/index.png", IsBinary: true},
		},
	}
}

func atlasStatuses() []model.CommitStatus {
	return []model.CommitStatus{
		{ID: 1, SHA: atlasHead, Context: "ci/build", State: model.CommitStatusSuccess, Description: "Build passed", TargetURL: "#", CreatedAt: ago(days(9)), UpdatedAt: ago(days(9))},
		{ID: 2, SHA: atlasHead, Context: "ci/lint", State: model.CommitStatusFailure, Description: "2 issues found", TargetURL: "#", CreatedAt: ago(days(9)), UpdatedAt: ago(days(9))},
	}
}

func contributorRows() []service.ContributorWithTimeline {
	return []service.ContributorWithTimeline{
		{Username: "ada-silva", Commits: 112, Additions: 8420, Deletions: 2310, Timeline: []int{4, 6, 2, 0, 7, 9, 3, 5, 8, 4, 6, 10}},
		{Username: fixtureUser, Commits: 64, Additions: 3105, Deletions: 1822, Timeline: []int{2, 1, 0, 3, 4, 2, 1, 0, 5, 2, 3, 4}},
		{Username: "li-wei", Commits: 21, Additions: 740, Deletions: 156, Timeline: []int{0, 0, 1, 0, 2, 3, 0, 1, 0, 0, 4, 2}},
	}
}

func last30(seed int) []int {
	out := make([]int, 30)
	for i := range out {
		out[i] = (i*seed + i/3) % 7
	}
	return out
}

func atlasEvents() []model.Event {
	repoID := int64(1)
	ev := func(id int64, actor, typ string, age time.Duration, payload string) model.Event {
		return model.Event{ID: id, ActorName: actor, RepoID: &repoID, RepoName: atlasName, OwnerName: fixtureUser,
			EventType: typ, CreatedAt: ago(age), Payload: []byte(payload)}
	}
	return []model.Event{
		ev(1, "ada-silva", model.EventPush, days(9), `{"branch":"main","commit_total":2,"commits":[{"sha":"3f9c2ab","message":"Cache tile index on disk (#41)"},{"sha":"e2d1c0b","message":"Reload the index when the pack file changes"}]}`),
		ev(2, "li-wei", model.EventPROpened, days(10), `{"number":41,"title":"Cache tile index on disk"}`),
		ev(3, "mkappworks", model.EventComment, days(11), `{"number":38,"kind":"issue","body":"Reproduced on the fleet laptop, the index rebuild takes 40s."}`),
		ev(4, "ada-silva", model.EventIssueOpened, days(12), `{"number":38,"title":"Index rebuild is slow on start"}`),
		ev(5, "mkappworks", model.EventPRMerged, days(14), `{"number":37}`),
		ev(6, "mkappworks", model.EventReleasePublished, days(30), `{"tag":"v0.4.0","name":"Offline index"}`),
		ev(7, "li-wei", model.EventIssueClosed, days(35), `{"number":31}`),
	}
}

func codeSearchResults() []model.CodeSearchResult {
	m := func(s string) string { return model.SnippetMarkStart + s + model.SnippetMarkEnd }
	return []model.CodeSearchResult{
		{RepoID: 1, RepoName: atlasName, OwnerName: fixtureUser, FilePath: blobPath,
			Snippet: "// " + m("TileIndex") + " maps a tile key to the offset of its bytes in the pack file.\ntype " + m("TileIndex") + " struct {"},
		{RepoID: 1, RepoName: atlasName, OwnerName: fixtureUser, FilePath: "internal/service/tile_index_test.go",
			Snippet: "func Test" + m("TileIndex") + "_Lookup(t *testing.T) {\n\tidx := New" + m("TileIndex") + "()"},
		{RepoID: 2, RepoName: "rusty-meadow", OwnerName: fixtureUser, FilePath: "docs/search.md",
			Snippet: "The " + m("tile index") + " is rebuilt on every build."},
	}
}

func searchResults(query string) *service.SearchResults {
	repos := fixtureRepos()[:2]
	return &service.SearchResults{
		Query: query, Type: "all", Repos: repos,
		Orgs:  []model.Organization{{ID: 1, Name: "paper-planes", DisplayName: "Paper Planes"}},
		Users: []model.User{{ID: 2, Username: "ada-silva", Bio: "Maps, tiles and offline sync."}, {ID: 3, Username: "atlas-bot", Bio: "Release notes bot."}},
		Issues: []model.Issue{
			{Number: 38, Title: "Index rebuild is slow on start", State: model.IssueStateOpen, AuthorName: "ada-silva", Visibility: "public",
				CreatedAt: ago(days(12)), RepoOwner: fixtureUser, RepoName: atlasName},
			{Number: 31, Title: "Atlas pack rollover loses the last tile", State: model.IssueStateClosed, AuthorName: "li-wei", Visibility: "public",
				CreatedAt: ago(days(40)), RepoOwner: fixtureUser, RepoName: atlasName},
		},
		Pulls: []model.PullRequest{
			{Number: 41, Title: "Cache tile index on disk", State: model.PRStateOpen, AuthorName: "li-wei", CreatedAt: ago(days(10)),
				RepoOwner: fixtureUser, RepoName: atlasName},
		},
	}
}

func topicRepos() []model.RepositoryWithStats {
	repos := fixtureRepos()
	return []model.RepositoryWithStats{
		{Repository: repos[0], StarCount: 128}, {Repository: repos[1], StarCount: 54}, {Repository: repos[3], StarCount: 9},
	}
}

func stargazerUsers() []model.User {
	return []model.User{
		{ID: 2, Username: "ada-silva", Bio: "Maps, tiles and offline sync."}, {ID: 3, Username: "li-wei"},
		{ID: 4, Username: "amara-silva", Bio: "Platform engineer."},
	}
}
