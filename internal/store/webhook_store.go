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

func (s *WebhookStore) Create(ctx context.Context, wh *model.Webhook) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO webhooks (repo_id, url, secret, events, active) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		wh.RepoID, wh.URL, wh.Secret, wh.Events, wh.Active,
	).Scan(&wh.ID)
	if err != nil {
		return fmt.Errorf("webhook create: %w", err)
	}
	return nil
}

func (s *WebhookStore) ListByRepo(ctx context.Context, repoID int64) ([]model.Webhook, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, url, secret, events, active, created_at, updated_at
		 FROM webhooks WHERE repo_id = $1 ORDER BY created_at DESC`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook list by repo: %w", err)
	}
	defer rows.Close()
	var hooks []model.Webhook
	for rows.Next() {
		var wh model.Webhook
		if err := rows.Scan(&wh.ID, &wh.RepoID, &wh.URL, &wh.Secret, &wh.Events, &wh.Active, &wh.CreatedAt, &wh.UpdatedAt); err != nil {
			return nil, err
		}
		hooks = append(hooks, wh)
	}
	return hooks, rows.Err()
}

func (s *WebhookStore) GetByID(ctx context.Context, id int64) (*model.Webhook, error) {
	wh := &model.Webhook{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, url, secret, events, active, created_at, updated_at FROM webhooks WHERE id = $1`,
		id,
	).Scan(&wh.ID, &wh.RepoID, &wh.URL, &wh.Secret, &wh.Events, &wh.Active, &wh.CreatedAt, &wh.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("webhook get by id: %w", err)
	}
	return wh, nil
}

func (s *WebhookStore) Delete(ctx context.Context, id, repoID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM webhooks WHERE id = $1 AND repo_id = $2`,
		id, repoID,
	)
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
