package model

import "time"

const (
	PackageTypeContainer = "container"
	PackageTypeGeneric   = "generic"
)

// Package is a named artifact under a path owner (a user or an organization). RepoID is nil while the
// package is pending: created by a blob upload, not yet linked by a manifest push.
type Package struct {
	ID               int64
	OwnerUserID      *int64
	OwnerOrgID       *int64
	Type             string
	Name             string
	RepoID           *int64
	CreatorID        *int64
	PendingExpiresAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// RegistryBlob is one stored object, shared by every package that links its digest.
type RegistryBlob struct {
	Digest     string
	Size       int64
	UnlinkedAt *time.Time
	CreatedAt  time.Time
}

// PackageManifest keeps the exact bytes pushed, because its digest is over them.
type PackageManifest struct {
	ID            int64
	PackageID     int64
	Digest        string
	MediaType     string
	Size          int64
	Content       []byte
	SubjectDigest string
	ArtifactType  string
	Platforms     string
	PullCount     int64
	CreatedAt     time.Time
}

type PackageTag struct {
	ID         int64
	PackageID  int64
	Name       string
	ManifestID int64
	PusherID   *int64
	PullCount  int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// UploadSession is an open blob upload; TempPath is a local spool file.
type UploadSession struct {
	ID        string
	PackageID int64
	UserID    int64
	Bytes     int64
	TempPath  string
	CreatedAt time.Time
}

type PackageVersion struct {
	ID        int64
	PackageID int64
	Version   string
	CreatedAt time.Time
}

type PackageFile struct {
	ID         int64
	VersionID  int64
	Filename   string
	Size       int64
	SHA256     string
	StorageKey string
	UploaderID *int64
	CreatedAt  time.Time
}

// GenericPackageSummary is a generic package with what its list row shows.
type GenericPackageSummary struct {
	Package
	LatestVersion string
	VersionCount  int
	TotalSize     int64
}

// GenericVersionFiles is one version of a generic package with its files.
type GenericVersionFiles struct {
	PackageVersion
	Files []PackageFile
}

// PackageSummary is a linked package with what a list row shows. RepoOwner and RepoName name the Repository it is
// linked to now, which after a transfer differs from OwnerName, the path owner. For a container LatestVersion is the
// most recently pushed tag, and Pulls counts manifest pulls; a generic package has no download counts.
type PackageSummary struct {
	Package
	OwnerName     string
	RepoOwner     string
	RepoName      string
	Size          int64
	TagCount      int
	VersionCount  int
	LatestVersion string
	Pulls         int64
}

// TagRow is a tag of a container package. Size is the distinct blob bytes of the image, or of every child of an index.
type TagRow struct {
	Name      string
	Digest    string
	MediaType string
	Size      int64
	Platforms []string
	Publisher string
	PullCount int64
	UpdatedAt time.Time
}

// ManifestRow is a manifest no tag points at.
type ManifestRow struct {
	Digest       string
	MediaType    string
	Size         int64
	ArtifactType string
	Platforms    []string
	PullCount    int64
	CreatedAt    time.Time
}
