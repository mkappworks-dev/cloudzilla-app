package service

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// AuditService records security-relevant events to the audit_log table.
// AuditService records and retrieves audit log entries for superadmin review.
type AuditService struct {
	store *store.AuditLogStore
}

// NewAuditService creates a new AuditService.
// NewAuditService creates an AuditService backed by the given audit log store.
func NewAuditService(s *store.AuditLogStore) *AuditService {
	return &AuditService{store: s}
}

// Record writes an audit entry asynchronously (fire-and-forget via goroutine).
// actorID = 0 means an unauthenticated actor.
// targetID = 0 means no specific target record.
func (s *AuditService) Record(
	ctx context.Context,
	r *http.Request,
	actorID int64,
	actorName, action, targetType string,
	targetID int64,
	targetName string,
	metadata map[string]any,
) {
	ip := extractIP(r)
	ua := r.UserAgent()

	var aid *int64
	if actorID != 0 {
		aid = &actorID
	}
	var tid *int64
	if targetID != 0 {
		tid = &targetID
	}

	entry := &model.AuditEntry{
		ActorID:    aid,
		ActorName:  actorName,
		Action:     action,
		TargetType: targetType,
		TargetID:   tid,
		TargetName: targetName,
		IPAddress:  ip,
		UserAgent:  ua,
		Metadata:   metadata,
	}

	go func() {
		// Use a background context so the record outlives the request context.
		if err := s.store.Create(context.Background(), entry); err != nil {
			slog.Error("audit log write failed",
				"action", entry.Action,
				"actor_id", entry.ActorID,
				"actor_name", entry.ActorName,
				"error", err,
			)
		}
	}()
}

// List returns a filtered, paginated slice of audit entries and the total count.
func (s *AuditService) List(ctx context.Context, f model.AuditFilter, page, pageSize int) ([]model.AuditEntry, int, error) {
	entries, err := s.store.List(ctx, f, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.Count(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// extractIP reads the real client IP from X-Forwarded-For or RemoteAddr.
func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
