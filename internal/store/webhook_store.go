package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// WebhookStore provides database operations for webhooks and delivery history.
type WebhookStore struct{ db *sql.DB }

// NewWebhookStore creates a WebhookStore backed by the given database.
func NewWebhookStore(db *sql.DB) *WebhookStore { return &WebhookStore{db: db} }

const webhookColumns = `id, repo_id, org_id, url, secret, events, active, created_at, updated_at`

func scanWebhook(sc interface{ Scan(...any) error }) (model.Webhook, error) {
	var wh model.Webhook
	var repoID, orgID sql.NullInt64
	if err := sc.Scan(&wh.ID, &repoID, &orgID, &wh.URL, &wh.Secret, &wh.Events, &wh.Active, &wh.CreatedAt, &wh.UpdatedAt); err != nil {
		return wh, err
	}
	wh.RepoID, wh.OrgID = repoID.Int64, orgID.Int64
	return wh, nil
}

func (s *WebhookStore) list(ctx context.Context, what, where string, arg int64) ([]model.Webhook, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+webhookColumns+` FROM webhooks WHERE `+where+` ORDER BY created_at DESC`, arg)
	if err != nil {
		return nil, fmt.Errorf("webhook list %s: %w", what, err)
	}
	defer rows.Close()
	var hooks []model.Webhook
	for rows.Next() {
		wh, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, wh)
	}
	return hooks, rows.Err()
}

// Create stores wh under its RepoID or its OrgID, whichever is set; the table's CHECK refuses both or neither.
func (s *WebhookStore) Create(ctx context.Context, wh *model.Webhook) error {
	var repoID, orgID sql.NullInt64
	if wh.RepoID != 0 {
		repoID = sql.NullInt64{Int64: wh.RepoID, Valid: true}
	}
	if wh.OrgID != 0 {
		orgID = sql.NullInt64{Int64: wh.OrgID, Valid: true}
	}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO webhooks (repo_id, org_id, url, secret, events, active) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		repoID, orgID, wh.URL, wh.Secret, wh.Events, wh.Active,
	).Scan(&wh.ID)
	if err != nil {
		return fmt.Errorf("webhook create: %w", err)
	}
	return nil
}

func (s *WebhookStore) ListByRepo(ctx context.Context, repoID int64) ([]model.Webhook, error) {
	return s.list(ctx, "by repo", "repo_id = $1", repoID)
}

func (s *WebhookStore) ListByOrg(ctx context.Context, orgID int64) ([]model.Webhook, error) {
	return s.list(ctx, "by org", "org_id = $1", orgID)
}

// ListForRepoEvents returns the repository's own hooks and those of the organization that owns it now, so a
// transfer changes which hooks fire without moving any rows.
func (s *WebhookStore) ListForRepoEvents(ctx context.Context, repoID int64) ([]model.Webhook, error) {
	return s.list(ctx, "for repo events",
		"repo_id = $1 OR org_id = (SELECT org_id FROM repositories WHERE id = $1)", repoID)
}

func (s *WebhookStore) CountByOrg(ctx context.Context, orgID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhooks WHERE org_id = $1`, orgID).Scan(&n)
	return n, err
}

func (s *WebhookStore) GetByID(ctx context.Context, id int64) (*model.Webhook, error) {
	wh, err := scanWebhook(s.db.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhooks WHERE id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("webhook get by id: %w", err)
	}
	return &wh, nil
}

func (s *WebhookStore) Delete(ctx context.Context, id, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM webhooks WHERE id = $1 AND repo_id = $2`,
		id, repoID,
	)
	return err
}

func (s *WebhookStore) DeleteForOrg(ctx context.Context, id, orgID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = $1 AND org_id = $2`, id, orgID)
	return err
}

func (s *WebhookStore) Update(ctx context.Context, wh *model.Webhook) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET url=$1, secret=$2, events=$3, active=$4, updated_at=NOW() WHERE id=$5`,
		wh.URL, wh.Secret, wh.Events, wh.Active, wh.ID,
	)
	return err
}

func (s *WebhookStore) LogDelivery(ctx context.Context, d *model.WebhookDelivery) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO webhook_deliveries (webhook_id, event, payload, response_code, response_body, error)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		d.WebhookID, d.Event, d.Payload, d.ResponseCode, d.ResponseBody, d.Error,
	).Scan(&d.ID)
	if err != nil {
		return fmt.Errorf("webhook log delivery: %w", err)
	}
	return nil
}

func (s *WebhookStore) ListDeliveries(ctx context.Context, webhookID int64) ([]model.WebhookDelivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, webhook_id, event, payload, response_code, response_body, error, delivered_at
		 FROM webhook_deliveries WHERE webhook_id = $1 ORDER BY delivered_at DESC LIMIT 50`,
		webhookID,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook list deliveries: %w", err)
	}
	defer rows.Close()
	var deliveries []model.WebhookDelivery
	for rows.Next() {
		var d model.WebhookDelivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &d.Payload, &d.ResponseCode, &d.ResponseBody, &d.Error, &d.DeliveredAt); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// ListPendingRetry returns deliveries whose next_retry_at is due and attempt_count < 5.
func (s *WebhookStore) ListPendingRetry(ctx context.Context) ([]model.WebhookDelivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, webhook_id, event, payload, response_code, response_body, error, delivered_at, attempt_count, next_retry_at
		 FROM webhook_deliveries
		 WHERE next_retry_at IS NOT NULL AND next_retry_at <= NOW() AND attempt_count < 5
		 ORDER BY next_retry_at ASC
		 LIMIT 100`,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook list pending retry: %w", err)
	}
	defer rows.Close()
	var deliveries []model.WebhookDelivery
	for rows.Next() {
		var d model.WebhookDelivery
		var nextRetryAt sql.NullTime
		if err := rows.Scan(
			&d.ID, &d.WebhookID, &d.Event, &d.Payload,
			&d.ResponseCode, &d.ResponseBody, &d.Error, &d.DeliveredAt,
			&d.AttemptCount, &nextRetryAt,
		); err != nil {
			return nil, err
		}
		if nextRetryAt.Valid {
			d.NextRetryAt = &nextRetryAt.Time
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// UpdateDeliveryRetry updates retry tracking columns after an attempt.
// Pass nextRetryAt=nil to clear the retry (success or max attempts reached).
func (s *WebhookStore) UpdateDeliveryRetry(ctx context.Context, deliveryID int64, nextRetryAt *time.Time, attemptCount int, responseCode int, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries
		 SET attempt_count=$1, next_retry_at=$2, response_code=$3, error=$4
		 WHERE id=$5`,
		attemptCount, nextRetryAt, responseCode, errMsg, deliveryID,
	)
	return err
}

// UpdateWebhookEvents updates the events filter string for a webhook.
func (s *WebhookStore) UpdateWebhookEvents(ctx context.Context, webhookID, repoID int64, events string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET events=$1, updated_at=NOW() WHERE id=$2 AND repo_id=$3`,
		events, webhookID, repoID,
	)
	return err
}

// UpdateWebhookEventsForOrg is UpdateWebhookEvents for an organization webhook.
func (s *WebhookStore) UpdateWebhookEventsForOrg(ctx context.Context, webhookID, orgID int64, events string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET events=$1, updated_at=NOW() WHERE id=$2 AND org_id=$3`,
		events, webhookID, orgID,
	)
	return err
}

// ListDeliveriesWithRetry returns deliveries including retry columns.
func (s *WebhookStore) ListDeliveriesWithRetry(ctx context.Context, webhookID int64) ([]model.WebhookDelivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, webhook_id, event, payload, response_code, response_body, error, delivered_at, attempt_count, next_retry_at
		 FROM webhook_deliveries WHERE webhook_id = $1 ORDER BY delivered_at DESC LIMIT 50`,
		webhookID,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook list deliveries: %w", err)
	}
	defer rows.Close()
	var deliveries []model.WebhookDelivery
	for rows.Next() {
		var d model.WebhookDelivery
		var nextRetryAt sql.NullTime
		if err := rows.Scan(
			&d.ID, &d.WebhookID, &d.Event, &d.Payload,
			&d.ResponseCode, &d.ResponseBody, &d.Error, &d.DeliveredAt,
			&d.AttemptCount, &nextRetryAt,
		); err != nil {
			return nil, err
		}
		if nextRetryAt.Valid {
			d.NextRetryAt = &nextRetryAt.Time
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// GetDeliveryByID returns a single webhook delivery by ID.
func (s *WebhookStore) GetDeliveryByID(ctx context.Context, id int64) (*model.WebhookDelivery, error) {
	d := &model.WebhookDelivery{}
	var nextRetryAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, webhook_id, event, payload, response_code, response_body, error, delivered_at, attempt_count, next_retry_at
		 FROM webhook_deliveries WHERE id = $1`,
		id,
	).Scan(
		&d.ID, &d.WebhookID, &d.Event, &d.Payload,
		&d.ResponseCode, &d.ResponseBody, &d.Error, &d.DeliveredAt,
		&d.AttemptCount, &nextRetryAt,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook delivery get: %w", err)
	}
	if nextRetryAt.Valid {
		d.NextRetryAt = &nextRetryAt.Time
	}
	return d, nil
}
