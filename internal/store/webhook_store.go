package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkappworks/cloudzilla/internal/model"
)

type WebhookStore struct{ db *sql.DB }

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
