package view

import (
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
)

type RepoData struct {
	BasePage
	Repo          model.Repository
	Owner         string
	RepoName      string
	CloneHTTP     string
	CloneSSH      string
	CanWrite      bool
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
}

// Releases page
type ReleasesData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Releases []model.Release
	CanWrite bool
}

// Release detail page
type ReleaseDetailData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Release  model.Release
	BodyHTML  string
	CanWrite bool
}

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

type BranchProtectionsFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Rules     []*model.BranchProtection
	CanManage bool
}

type DeployKeysFragData struct {
	Owner      string
	RepoName   string
	RepoID     int64
	DeployKeys []model.DeployKey
	CanManage  bool
}

type WebhooksFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Webhooks  []model.Webhook
	CanManage bool
}

type WebhookDeliveriesFragData struct {
	Owner      string
	RepoName   string
	WebhookID  int64
	Deliveries []model.WebhookDelivery
	CanManage  bool
}

type RefsData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Branches []service.BranchInfo
	Tags     []service.TagInfo
	CanWrite bool
}

type BranchesFragData struct {
	Owner         string
	RepoName      string
	Branches      []service.BranchInfo
	CanWrite      bool
	DefaultBranch string
}

type TagsFragData struct {
	Owner    string
	RepoName string
	Tags     []service.TagInfo
	CanWrite bool
}

type TreeData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	Ref         string
	Path        string
	Breadcrumbs []service.BreadcrumbPart
	Entries     []service.TreeEntry
	RefsURL     string
}

type BlobData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	Ref         string
	Path        string
	Breadcrumbs []service.BreadcrumbPart
	Lines       []service.CodeLine
	IsBinary    bool
	BlameURL    string
}

type BlameData struct {
	BasePage
	Repo        model.Repository
	Owner       string
	RepoName    string
	Ref         string
	Path        string
	Breadcrumbs []service.BreadcrumbPart
	Lines       []service.BlameLine
	BlobURL     string
}

type CommitsData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Log      *service.CommitLog
	RefsURL  string
}

type CommitData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Commit   *service.CommitDetail
	Statuses []model.CommitStatus
}

type RepoCollaboratorsFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Collabs   []model.Permission
	CanManage bool
}

// Repo labels management (settings page)
type RepoLabelsFragData struct {
	Owner    string
	RepoName string
	RepoID   int64
	Labels   []model.Label
	CanWrite bool
}

// Fork button fragment
type ForkButtonData struct {
	BasePage
	Owner     string
	RepoName  string
	ForkCount int
}

// Star button fragment
type StarButtonData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Count     int
	IsStarred bool
	LoggedIn  bool
}

// Watch button fragment
type WatchButtonData struct {
	Owner    string
	RepoName string
	RepoID   int64
	Level    string
	LoggedIn bool
}

// RepoTopicsFragData is the view model for the repo-topics HTMX fragment.
type RepoTopicsFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Topics    []model.Topic
	CanManage bool
}
