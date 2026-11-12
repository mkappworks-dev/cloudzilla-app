package model

import "time"

type AccountExportStatus string

const (
	ExportRequested AccountExportStatus = "requested"
	ExportRunning   AccountExportStatus = "running"
	ExportReady     AccountExportStatus = "ready"
	ExportFailed    AccountExportStatus = "failed"
)

// AccountExport is one request for a zip of a user's data. StorageKey is set only when Status is ready.
type AccountExport struct {
	ID          int64
	UserID      int64
	Status      AccountExportStatus
	StorageKey  string
	Error       string
	RequestedAt time.Time
	StartedAt   *time.Time
	ReadyAt     *time.Time
	ExpiresAt   *time.Time
}
