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
