package store

import "database/sql"

// RegistryStore persists registry blobs, manifests, tags and upload sessions.
type RegistryStore struct{ db *sql.DB }

func NewRegistryStore(db *sql.DB) *RegistryStore { return &RegistryStore{db: db} }
