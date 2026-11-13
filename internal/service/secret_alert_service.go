package service

import (
	"context"
	"errors"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrSecretAlertNotFound = store.ErrSecretAlertNotFound
	ErrSecretAlertState    = store.ErrSecretAlertState
	ErrSecretAllowRefused  = errors.New("only a false positive or a test value can be allowed, and not a Cloudzilla token")
	ErrSecretResolution    = errors.New("unknown resolution")
)

const secretAlertsPerPage = 25

// SecretAlertService records and resolves secret scan alerts. It records the actor and checks nothing about them:
// authorisation is the handler's.
type SecretAlertService struct{ alerts *store.SecretAlertStore }

func NewSecretAlertService(alerts *store.SecretAlertStore) *SecretAlertService {
	return &SecretAlertService{alerts: alerts}
}

// Record stores one sighting of a finding: a new alert, or a location and event on the alert for the same value.
// kind is model.SecretEventFound or model.SecretEventBlocked; the bool reports a new alert.
func (s *SecretAlertService) Record(ctx context.Context, repoID int64, f secretscan.Finding, loc model.SecretSighting, kind string) (*model.SecretScanAlert, bool, error) {
	if kind != model.SecretEventFound && kind != model.SecretEventBlocked {
		return nil, false, errors.New("secret alert record: kind must be found or blocked")
	}
	loc.Line = f.Line
	return s.alerts.Record(ctx, repoID, f.Type, secretscan.Hash(repoID, f.Value), secretscan.Mask(f.Value), kind, loc)
}

// Close closes an open alert with a resolution: revoked, false_positive or used_in_tests.
func (s *SecretAlertService) Close(ctx context.Context, alertID, actorID int64, resolution string) error {
	if !validResolution(resolution) {
		return ErrSecretResolution
	}
	return s.alerts.Close(ctx, alertID, actorID, resolution, model.SecretEventClosed, false)
}

func (s *SecretAlertService) Reopen(ctx context.Context, alertID, actorID int64) error {
	return s.alerts.Reopen(ctx, alertID, actorID)
}

// Allow closes an alert so its value stops being blocked. A live Cloudzilla token cannot be allowed.
func (s *SecretAlertService) Allow(ctx context.Context, alertID, actorID int64, resolution string) error {
	if resolution != model.SecretResolutionFalsePositive && resolution != model.SecretResolutionUsedInTests {
		return ErrSecretAllowRefused
	}
	alert, err := s.alerts.GetByID(ctx, alertID)
	if err != nil {
		return err
	}
	if alert.SecretType == model.SecretTypeCloudzillaToken {
		return ErrSecretAllowRefused
	}
	return s.alerts.Close(ctx, alertID, actorID, resolution, model.SecretEventAllowed, false)
}

// AutoRevoke closes the alert of a Cloudzilla token the instance has just deleted, with no acting user.
func (s *SecretAlertService) AutoRevoke(ctx context.Context, alertID int64) error {
	return s.alerts.Close(ctx, alertID, 0, model.SecretResolutionRevoked, model.SecretEventAutoRevoked, true)
}

// MarkReached records that the commits are now reachable from a ref.
func (s *SecretAlertService) MarkReached(ctx context.Context, repoID int64, commitSHAs []string) error {
	return s.alerts.MarkReached(ctx, repoID, commitSHAs)
}

// IsClosed reports whether the value's hash has a closed alert in the repository, which keeps it from being blocked
// or alerted again.
func (s *SecretAlertService) IsClosed(ctx context.Context, repoID int64, hash string) (bool, error) {
	return s.alerts.IsClosed(ctx, repoID, hash)
}

// List returns a page (1-based) of alerts and the total across pages. An empty state or secretType matches all.
func (s *SecretAlertService) List(ctx context.Context, repoID int64, state, secretType string, page int) ([]model.SecretScanAlert, int, error) {
	return s.alerts.List(ctx, repoID, state, secretType, page, secretAlertsPerPage)
}

func (s *SecretAlertService) Get(ctx context.Context, repoID int64, number int) (*model.SecretScanAlert, error) {
	return s.alerts.Get(ctx, repoID, number)
}

func (s *SecretAlertService) Locations(ctx context.Context, alertID int64) ([]model.SecretScanLocation, error) {
	return s.alerts.Locations(ctx, alertID)
}

func (s *SecretAlertService) Events(ctx context.Context, alertID int64) ([]model.SecretScanEvent, error) {
	return s.alerts.Events(ctx, alertID)
}

// OpenCount counts the open alerts a ref reaches, for the Security tab badge.
func (s *SecretAlertService) OpenCount(ctx context.Context, repoID int64) (int, error) {
	return s.alerts.OpenCount(ctx, repoID)
}

// SweepBlocked deletes open alerts older than olderThan that no ref ever reached, and returns how many.
func (s *SecretAlertService) SweepBlocked(ctx context.Context, olderThan time.Time) (int, error) {
	return s.alerts.SweepBlocked(ctx, olderThan)
}

func validResolution(r string) bool {
	return r == model.SecretResolutionRevoked || r == model.SecretResolutionFalsePositive || r == model.SecretResolutionUsedInTests
}
