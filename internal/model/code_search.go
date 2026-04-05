package model

// CodeSearchResult is a single hit returned by the code search store.
// CodeSearchResult represents a single file match from a code search query.
type CodeSearchResult struct {
	RepoID    int64   `db:"repo_id"`
	RepoName  string  `db:"repo_name"`
	OwnerName string  `db:"owner_name"`
	FilePath  string  `db:"file_path"`
	Snippet   string  `db:"snippet"`
	Rank      float64 `db:"rank"`
}
