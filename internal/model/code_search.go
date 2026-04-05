package model

// CodeSearchResult is a single hit returned by the code search store.
type CodeSearchResult struct {
	RepoID    int64   `db:"repo_id"`
	RepoName  string  `db:"repo_name"`
	OwnerName string  `db:"owner_name"`
	FilePath  string  `db:"file_path"`
	Snippet   string  `db:"snippet"`
	Rank      float64 `db:"rank"`
}
