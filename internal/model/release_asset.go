package model

import "time"

// ReleaseAsset is a file uploaded to a release. StorageKey never leaves the
// server: downloads go through the release's tag and the asset's name.
type ReleaseAsset struct {
	ID         int64     `json:"id"`
	ReleaseID  int64     `json:"release_id"`
	RepoID     int64     `json:"-"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256"`
	StorageKey string    `json:"-"`
	UploaderID *int64    `json:"uploader_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	// DownloadURL is filled in by the handler, which knows the owner, repo and tag.
	DownloadURL string `json:"download_url,omitempty"`
}
