package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type AdminModeService struct {
	store *store.AdminModeStore
	audit *AuditService
	ttl   time.Duration
	now   func() time.Time
}

// DefaultAdminModeTTL applies when auth.admin_mode_ttl is unset, so a config built
// in code never yields an admin mode that expires as it starts.
const DefaultAdminModeTTL = time.Hour

func NewAdminModeService(s *store.AdminModeStore, audit *AuditService, ttl time.Duration) *AdminModeService {
	if ttl <= 0 {
		ttl = DefaultAdminModeTTL
	}
	return &AdminModeService{store: s, audit: audit, ttl: ttl, now: time.Now}
}

func (s *AdminModeService) WithClock(now func() time.Time) *AdminModeService {
	s.now = now
	return s
}

func hashAdminModeToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Enter expects the caller to have confirmed the user's identity; a later sessionVersion bump ends the mode.
func (s *AdminModeService) Enter(ctx context.Context, r *http.Request, userID int64, username string, sessionVersion int) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, fmt.Errorf("admin mode token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	expiresAt := s.now().Add(s.ttl)
	if err := s.store.Create(ctx, userID, hashAdminModeToken(raw), sessionVersion, expiresAt); err != nil {
		return "", time.Time{}, err
	}
	s.record(ctx, r, userID, username, model.AuditActionAdminModeEnter, map[string]any{"expires_at": expiresAt.UTC().Format(time.RFC3339)})
	return raw, expiresAt, nil
}

// Resolve ends and audits a dead admin mode it finds, so the log shows how each one finished.
func (s *AdminModeService) Resolve(ctx context.Context, r *http.Request, raw string, userID int64, username string, sessionVersion int) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	hash := hashAdminModeToken(raw)
	now := s.now()
	expiresAt, live, err := s.store.Live(ctx, hash, userID, sessionVersion, now)
	if err != nil {
		slog.Error("admin mode lookup failed; treating as off", "user_id", userID, "error", err)
		return time.Time{}, false
	}
	if live {
		return expiresAt, true
	}
	s.end(ctx, r, hash, userID, username, "")
	return time.Time{}, false
}

func (s *AdminModeService) Leave(ctx context.Context, r *http.Request, raw string, userID int64, username string) {
	if raw == "" {
		return
	}
	s.end(ctx, r, hashAdminModeToken(raw), userID, username, "left")
}

func (s *AdminModeService) end(ctx context.Context, r *http.Request, hash string, userID int64, username, reason string) {
	now := s.now()
	expiresAt, ended, err := s.store.End(ctx, hash, userID, now)
	if err != nil {
		slog.Error("admin mode end failed", "user_id", userID, "error", err)
		return
	}
	if !ended {
		return
	}
	if reason == "" {
		reason = "revoked"
		if !expiresAt.After(now) {
			reason = "expired"
		}
	}
	s.record(ctx, r, userID, username, model.AuditActionAdminModeLeave, map[string]any{"reason": reason})
}

// RecordAction audits a mutating request that only passed because of admin mode.
func (s *AdminModeService) RecordAction(ctx context.Context, r *http.Request, userID int64, username, targetType, targetName string, status int) {
	if s.audit == nil || r == nil {
		return
	}
	meta := map[string]any{"method": r.Method, "path": r.URL.Path, "target_type": targetType, "target": targetName, "status": status}
	if targetType == "" {
		s.record(ctx, r, userID, username, model.AuditActionAdminModeAction, meta)
		return
	}
	// Named by what it touched, so the audit log shows which repository or organization; the ID is unknown here.
	s.audit.Record(ctx, r, userID, username, model.AuditActionAdminModeAction, targetType, 0, targetName, meta)
}

func (s *AdminModeService) record(ctx context.Context, r *http.Request, userID int64, username, action string, meta map[string]any) {
	if s.audit == nil || r == nil {
		return
	}
	s.audit.Record(ctx, r, userID, username, action, model.AuditTargetUser, userID, username, meta)
}
