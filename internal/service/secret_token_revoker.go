package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// TokenOutcome is what a czp_ value turned out to be.
type TokenOutcome int

const (
	// TokenUnknown is a value no stored hash matches, so nothing to alert on.
	TokenUnknown TokenOutcome = iota
	// TokenExpired is a stored token past its expiry: dead already, so it is not revoked.
	TokenExpired
	// TokenRevoked is a live token the resolver has just deleted.
	TokenRevoked
)

// TokenOutcomeResolver is a CloudzillaTokenResolver that tells an expired token from an unknown one.
type TokenOutcomeResolver interface {
	CloudzillaTokenResolver
	ResolveToken(ctx context.Context, repo *model.Repository, value string) (TokenOutcome, error)
}

// TokenRevokedNotifier tells a token's owner it was revoked. NotificationService implements it.
type TokenRevokedNotifier interface {
	NotifyTokenRevoked(ctx context.Context, ownerID int64, token model.AccessToken, repo model.Repository)
}

// TokenRevoker deletes the Cloudzilla access token a scan finds in a repository.
type TokenRevoker struct {
	tokens   *store.AccessTokenStore
	audit    *AuditService
	notifier TokenRevokedNotifier
}

func NewTokenRevoker(tokens *store.AccessTokenStore, audit *AuditService, notifier TokenRevokedNotifier) *TokenRevoker {
	return &TokenRevoker{tokens: tokens, audit: audit, notifier: notifier}
}

// ResolveLiveToken reports whether value was a live token, which it has then deleted.
func (r *TokenRevoker) ResolveLiveToken(ctx context.Context, repo *model.Repository, value string) (bool, error) {
	out, err := r.ResolveToken(ctx, repo, value)
	return out == TokenRevoked, err
}

// ResolveToken deletes the token value names when it is unexpired, then records the audit entry and tells the owner.
func (r *TokenRevoker) ResolveToken(ctx context.Context, repo *model.Repository, value string) (TokenOutcome, error) {
	sum := sha256.Sum256([]byte(value))
	t, err := r.tokens.GetByHash(ctx, hex.EncodeToString(sum[:]))
	if errors.Is(err, sql.ErrNoRows) {
		return TokenUnknown, nil
	}
	if err != nil {
		return TokenUnknown, err
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()) {
		return TokenExpired, nil
	}
	if err := r.tokens.Delete(ctx, t.ID, t.UserID); err != nil {
		return TokenUnknown, fmt.Errorf("revoke leaked token: %w", err)
	}
	// The token is already gone; a failed audit write must not turn the revoke into a failed push.
	if err := r.audit.RecordOffline(ctx, "system", model.AuditActionTokenAutoRevoke, model.AuditTargetUser, t.UserID, t.Name,
		map[string]any{"repo": repo.OwnerName + "/" + repo.Name, "token_id": t.ID}); err != nil {
		slog.Error("secret-scan: audit token revoke", "token_id", t.ID, "error", err)
	}
	if r.notifier != nil {
		r.notifier.NotifyTokenRevoked(ctx, t.UserID, *t, *repo)
	}
	return TokenRevoked, nil
}
