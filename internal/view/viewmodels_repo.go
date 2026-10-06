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

// EditFileData holds template data for the edit-file page. Path is the file
// being edited; NewPath, Content and Message are what the form shows.
type EditFileData struct {
	BasePage
	Owner    string
	RepoName string
	Ref      string
	Path     string
	BlobSHA  string
	NewPath  string
	Content  string
	Message  string
	Error    string
	// ConflictURL links the current version when the file changed after the
	// page loaded.
	ConflictURL string
}

// RepoNewData holds template data for the new repository form page.
type RepoNewData struct {
	BasePage
	OwnedOrgs          []model.Organization
	GitignoreTemplates []string
	LicenseTemplates   []service.License

	// Prefill values read from query params (e.g. the profile-README CTA links
	// here with ?name=foo&visibility=public&init_readme=1).
	DefaultName       string
	DefaultPrivate    bool
	DefaultInitReadme bool
	// DefaultOwner preselects the owner dropdown. Empty falls back to the
	// signed-in user. Used by org pages that link here with ?owner=stitch-labs
	// so the new repo lands under the org the user came from.
	DefaultOwner string
}

type RepoForkData struct {
	BasePage
	SourceOwner   string
	SourceName    string
	Description   string
	Private       bool
	DefaultBranch string   // "" unless that branch exists: otherwise there is nothing to prune to
	Owners        []string // namespaces the fork may land in; empty when none qualifies
	ExistingForks []RepoRef
}

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
	PendingTransfer   *model.RepoTransfer // nil unless IsOwner and a transfer awaits its recipient
	// What the viewer confirms giving others access with.
	Confirm components.ConfirmFactors
}

// RepoTransfersData lists the repositories offered to the viewer.
type RepoTransfersData struct {
	BasePage
	Transfers []IncomingRepoTransfer
}

// IncomingRepoTransfer is an offer with the collaborators who would keep
// their access, which the recipient should see before accepting.
type IncomingRepoTransfer struct {
	model.RepoTransfer
	Collaborators []model.Permission
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

// TreeData holds template data for the repository tree browser page.
type TreeData struct {
	BasePage
	Repo          model.Repository
	Owner         string
	RepoName      string
	Ref           string
	Path          string
	Breadcrumbs   []service.BreadcrumbPart
	Entries       []service.TreeEntryWithLastCommit
	RefsURL       string
	Branches      []service.BranchInfo
	Tags          []service.TagInfo
	CanManage     bool
	Sidebar       []components.TreeNode
	SidebarHidden bool
	ExpandAllURL  string
	LatestCommit  TreeLatestCommit
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
	Repo          model.Repository
	Owner         string
	RepoName      string
	Ref           string
	Path          string
	Breadcrumbs   []service.BreadcrumbPart
	Branches      []service.BranchInfo
	Tags          []service.TagInfo
	Lines         []service.CodeLine
	IsBinary      bool
	Size          int64
	BlameURL      string
	RawURL        string
	EditURL       string
	DeleteURL     string
	BlobSHA       string
	CanEdit       bool
	CanDelete     bool
	CanManage     bool
	Sidebar       []components.TreeNode
	SidebarHidden bool
	ExpandAllURL  string
	LatestCommit  TreeLatestCommit
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
	Branches     []service.BranchInfo
	Tags         []service.TagInfo
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
	IsOwner   bool
	Confirm   components.ConfirmFactors
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

// RepoImportData holds template data for the import form; the Default*
// fields come from query params, so "Try again" can prefill it.
type RepoImportData struct {
	BasePage
	OwnedOrgs      []model.Organization
	DefaultOwner   string
	DefaultPrivate bool
	DefaultURL     string
	DefaultName    string
	// MirrorIntervals is empty when mirror.enabled is off, which hides the option.
	MirrorIntervals []IntervalOption
}

// IntervalOption is one choice of a sync-interval picker.
type IntervalOption struct {
	Value, Label string
	Selected     bool
}

// RepoImportStatusData holds template data for an import's status page.
type RepoImportStatusData struct {
	BasePage
	Job service.ImportJob
}
