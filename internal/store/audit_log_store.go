package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// AuditLogStore provides read/write access to the audit_log table.
type AuditLogStore struct{ db *sql.DB }

// NewAuditLogStore creates a new AuditLogStore.
func NewAuditLogStore(db *sql.DB) *AuditLogStore {
	return &AuditLogStore{db: db}
}

// Create inserts a new audit entry and populates e.ID.
func (s *AuditLogStore) Create(ctx context.Context, e *model.AuditEntry) error {
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		return fmt.Errorf("audit log marshal metadata: %w", err)
	}
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO audit_log
		    (actor_id, actor_name, action, target_type, target_id, target_name,
		     ip_address, user_agent, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id`,
		e.ActorID, e.ActorName, e.Action, e.TargetType, e.TargetID, e.TargetName,
		e.IPAddress, e.UserAgent, string(meta),
	).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("audit log create: %w", err)
	}
	return nil
}

// Count returns the total number of entries matching the filter.
func (s *AuditLogStore) Count(ctx context.Context, f model.AuditFilter) (int, error) {
	query, args := buildAuditQuery("SELECT COUNT(*) FROM audit_log", f, -1, -1)
	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("audit log count: %w", err)
	}
	return n, nil
}

// List returns a page of audit entries ordered by created_at DESC.
// page is 1-based; pageSize is the number of rows to return.
func (s *AuditLogStore) List(ctx context.Context, f model.AuditFilter, page, pageSize int) ([]model.AuditEntry, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	query, args := buildAuditQuery(
		`SELECT id, actor_id, actor_name, action, target_type, target_id, target_name,
		        ip_address, user_agent, metadata, created_at
		 FROM audit_log`,
		f, pageSize, offset,
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit log list: %w", err)
	}
	defer rows.Close()

	var entries []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		var metaRaw []byte
		if err := rows.Scan(
			&e.ID, &e.ActorID, &e.ActorName, &e.Action, &e.TargetType, &e.TargetID, &e.TargetName,
			&e.IPAddress, &e.UserAgent, &metaRaw, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("audit log scan: %w", err)
		}
		if len(metaRaw) > 0 {
			if err := json.Unmarshal(metaRaw, &e.Metadata); err != nil {
				slog.Warn("audit log metadata unmarshal failed", "entry_id", e.ID, "error", err)
			}
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit log list rows: %w", err)
	}
	return entries, nil
}

// buildAuditQuery builds a filtered SQL query for audit_log.
// Pass pageSize < 0 to omit LIMIT/OFFSET (used for COUNT queries).
func buildAuditQuery(base string, f model.AuditFilter, pageSize, offset int) (string, []any) {
	var (
		conds []string
		args  []any
		n     = 1
	)

	if f.ActorID != nil {
		conds = append(conds, fmt.Sprintf("actor_id = $%d", n))
		args = append(args, *f.ActorID)
		n++
	}
	if f.Action != "" {
		conds = append(conds, fmt.Sprintf("action = $%d", n))
		args = append(args, f.Action)
		n++
	}
	if f.TargetType != "" {
		conds = append(conds, fmt.Sprintf("target_type = $%d", n))
		args = append(args, f.TargetType)
		n++
	}

	q := base
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY created_at DESC"

	if pageSize > 0 {
		q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", n, n+1)
		args = append(args, pageSize, offset)
	}

	return q, args
}
