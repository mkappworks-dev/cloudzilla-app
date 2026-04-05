package model

// Topic represents a searchable tag associated with a repository.
type Topic struct {
	ID   int64  `db:"id"   json:"id"`
	Name string `db:"name" json:"name"`
}
