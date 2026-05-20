package view

import (
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// RepoData holds template data for the repository home page.
type RepoData struct {
	BasePage
	Repo          model.Repository
	Owner         string
	RepoName      string
	CloneHTTP     string
	CloneSSH      string
	CanWrite      bool
	CanManage     bool
	ReadmeHTML    string
	StarCount     int
	IsStarred     bool
	WatchLevel    string
	ForkCount     int
	IsFork        bool
	ForkOfPath    string
	LatestRelease *model.Release
	Topics        []model.Topic
	IsArchived    bool
	Languages     []components.LangBarItem
	TopContribs   []service.ContributorStat
	Releases      []model.Release
	Heatmap       map[time.Time]int
	Entries       []service.TreeEntryWithLastCommit
	LatestCommit  TreeLatestCommit
	ReadmeName    string
	Branches      []service.BranchInfo
	Tags          []service.TagInfo
	BranchCount   int
	TagCount      int
	CommitCount   int
	AllFiles      []string
}

// NewFileData holds template data for the create-file / upload page.
type NewFileData struct {
	BasePage
	Owner    string
	RepoName string
	Ref      string
	Dir      string // optional subdirectory the file lands in
}

// RepoNewData holds template data for the new repository form page.
type RepoNewData struct {
	BasePage
	OwnedOrgs []model.Organization
}

// ReleaseView decorates a model.Release with computed display fields.
type ReleaseView struct {
	model.Release
	IsLatest bool   // true for the single newest published, non-draft, non-prerelease release
	DiffURL  string // compare URL vs the previous release; "" if no compare route
}

// ReleasesData holds template data for the releases list page.
type ReleasesData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Releases []ReleaseView
	CanWrite bool
}

type ReleaseNewData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Branches []service.BranchInfo
}

type ReleaseBodyCardData struct {
	Owner     string
	RepoName  string
	ReleaseID int64
	Body      string
	BodyHTML  string
	CanWrite  bool
	Editing   bool
}

type ReleaseDetailData struct {
	BasePage
	Repo       model.Repository
	Owner      string
	RepoName   string
	Release    model.Release
	BodyHTML   string
	CanWrite   bool
	AuthorName string // resolved from Release.AuthorID
	IsLatest   bool   // true if this is the newest published non-draft non-prerelease release
	CommitSHA  string // full hash of the commit the tag points at; "" if tag cannot be resolved
}

// RepoSettingsData holds template data for the repository settings page.
type RepoSettingsData struct {
	BasePage
	Repo              model.Repository
	Owner             string
	RepoName          string
	Webhooks          []model.Webhook
	Collabs           []model.Permission
	Labels            []model.Label
	DeployKeys        []model.DeployKey
	BranchProtections []*model.BranchProtection
	CanManage         bool
	IsOwner           bool
	CanTransfer       bool
}

// BranchProtectionsFragData holds template data for the branch protections HTMX fragment.
type BranchProtectionsFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Rules     []*model.BranchProtection
	CanManage bool
}

// DeployKeysFragData holds template data for the deploy keys HTMX fragment.
type DeployKeysFragData struct {
	Owner      string
	RepoName   string
	RepoID     int64
	DeployKeys []model.DeployKey
	CanManage  bool
}

// WebhooksFragData holds template data for the webhooks list HTMX fragment.
type WebhooksFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Webhooks  []model.Webhook
	CanManage bool
}

// WebhookDeliveriesFragData holds template data for the webhook delivery history HTMX fragment.
type WebhookDeliveriesFragData struct {
	Owner      string
	RepoName   string
	WebhookID  int64
	Deliveries []model.WebhookDelivery
	CanManage  bool
}

// RefsData holds template data for the branches and tags page.
type RefsData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Branches []service.BranchInfo
	Tags     []service.TagInfo
	CanWrite bool
}

// BranchesFragData holds template data for the branches list HTMX fragment.
type BranchesFragData struct {
	Owner         string
	RepoName      string
	Branches      []service.BranchInfo
	CanWrite      bool
	DefaultBranch string
}

// TagsFragData holds template data for the tags list HTMX fragment.
type TagsFragData struct {
	Owner    string
	RepoName string
	Tags     []service.TagInfo
	CanWrite bool
}

// TreeFileView carries the data needed to render an inline file viewer inside
// the tree page (when the path resolves to a file rather than a directory).
type TreeFileView struct {
	Lines    []service.CodeLine
	IsBinary bool
	Size     int64
	FileName string
	BlameURL string
	RawURL   string
	EditURL  string
	CanWrite bool
}

// TreeData holds template data for the repository tree browser page.
type TreeData struct {
	BasePage
	Repo         model.Repository
	Owner        string
	RepoName     string
	Ref          string
	Path         string
	Breadcrumbs  []service.BreadcrumbPart
	Entries      []service.TreeEntryWithLastCommit
	RefsURL      string
	CanManage    bool
	Sidebar      []components.TreeNode
	LatestCommit TreeLatestCommit
	FileView     *TreeFileView
	ActiveFile   string
}

// TreeLatestCommit summarises the most recent commit touching anything in
// the current directory. Rendered as a sub-header row above the file listing.
type TreeLatestCommit struct {
	SHA       string // short SHA
	Message   string // first line of commit message
	Author    string
	AuthorURL string
	CommitURL string
	Timestamp time.Time
}

// BlobData holds template data for the file blob viewer page.
type BlobData struct {
	BasePage
	Repo         model.Repository
	Owner        string
	RepoName     string
	Ref          string
	Path         string
	Breadcrumbs  []service.BreadcrumbPart
	Lines        []service.CodeLine
	IsBinary     bool
	Size         int64
	BlameURL     string
	RawURL       string
	EditURL      string
	CanWrite     bool
	CanManage    bool
	LatestCommit TreeLatestCommit
}

// BlameData holds template data for the file blame page.
type BlameData struct {
	BasePage
	Repo         model.Repository
	Owner        string
	RepoName     string
	Ref          string
	Path         string
	Breadcrumbs  []service.BreadcrumbPart
	Lines        []service.BlameLine
	BlobURL      string
	Contributors int
	CanManage    bool
}

// CommitsData holds template data for the commit log page.
type CommitsData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Log      *service.CommitLog
	RefsURL  string
}

// CommitData holds template data for the single commit detail page.
type CommitData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Commit   *service.CommitDetail
	Statuses []model.CommitStatus
}

// RepoCollaboratorsFragData holds template data for the repository collaborators HTMX fragment.
type RepoCollaboratorsFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Collabs   []model.Permission
	CanManage bool
}

// Repo labels management (settings page)
// RepoLabelsFragData holds template data for the repository labels HTMX fragment.
type RepoLabelsFragData struct {
	Owner    string
	RepoName string
	RepoID   int64
	Labels   []model.Label
	CanWrite bool
}

// Fork button fragment
// ForkButtonData holds template data for the fork button HTMX fragment.
type ForkButtonData struct {
	BasePage
	Owner     string
	RepoName  string
	ForkCount int
}

// Star button fragment
// StarButtonData holds template data for the star/unstar button HTMX fragment.
type StarButtonData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Count     int
	IsStarred bool
	LoggedIn  bool
}

// Watch button fragment
// WatchButtonData holds template data for the watch/unwatch button HTMX fragment.
type WatchButtonData struct {
	Owner    string
	RepoName string
	RepoID   int64
	Level    string
	LoggedIn bool
	Count    int
}

// RepoTopicsFragData is the view model for the repo-topics HTMX fragment.
// RepoTopicsFragData holds template data for the repository topics HTMX fragment.
type RepoTopicsFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Topics    []model.Topic
	CanManage bool
}
