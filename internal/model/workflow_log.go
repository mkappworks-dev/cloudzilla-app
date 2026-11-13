package model

import "time"

// WorkflowLogChunk is one posted piece of a Job's log: whole lines FirstLine..FirstLine+LineCount-1, kept as one object.
type WorkflowLogChunk struct {
	ID         int64
	JobID      int64
	RepoID     int64
	Seq        int
	FirstLine  int
	LineCount  int
	SizeBytes  int
	StorageKey string
	CreatedAt  time.Time
}

// WorkflowLogState is what a Job's chunks hold so far; the counters outlive the chunks' expiry.
type WorkflowLogState struct {
	Bytes     int64
	Lines     int
	Truncated bool
}

// WorkflowLogPage is a window of a Job's log by line offset. Lines is empty and NextOffset equals Offset when the
// reader is caught up.
type WorkflowLogPage struct {
	Offset     int      `json:"offset"`
	NextOffset int      `json:"next_offset"`
	TotalLines int      `json:"total_lines"`
	Truncated  bool     `json:"truncated"`
	Lines      []string `json:"lines"`
}

// WorkflowArtifact is an archive a Job uploaded. StorageKey never leaves the server.
type WorkflowArtifact struct {
	ID         int64     `json:"id"`
	RunID      int64     `json:"run_id"`
	JobID      int64     `json:"job_id"`
	RepoID     int64     `json:"-"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256"`
	StorageKey string    `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	// DownloadURL is filled in by the handler, which knows the owner and repo.
	DownloadURL string `json:"download_url,omitempty"`
}
