package model

import (
	"path"
	"time"
)

// Attachment is an image stored for a repo's markdown. Its Token names it in
// URLs; who may read it follows from the repo.
type Attachment struct {
	Token       string
	RepoID      int64
	UploaderID  *int64
	StorageKey  string
	ContentType string
	Size        int64
	CreatedAt   time.Time
}

// URL is the path markdown links to.
func (a Attachment) URL() string {
	return "/attachments/" + a.Token + path.Ext(a.StorageKey)
}
