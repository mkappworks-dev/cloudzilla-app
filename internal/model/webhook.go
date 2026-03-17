package model

import "time"

type Webhook struct {
	ID        int64     `db:"id"         json:"id"`
	RepoID    int64     `db:"repo_id"    json:"repo_id"`
	URL       string    `db:"url"        json:"url"`
	Secret    string    `db:"secret"     json:"-"`
	Events    string    `db:"events"     json:"events"`
	Active    bool      `db:"active"     json:"active"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

type WebhookDelivery struct {
	ID           int64     `db:"id"            json:"id"`
	WebhookID    int64     `db:"webhook_id"    json:"webhook_id"`
	Event        string    `db:"event"         json:"event"`
	Payload      string    `db:"payload"       json:"payload"`
	ResponseCode int       `db:"response_code" json:"response_code"`
	ResponseBody string    `db:"response_body" json:"response_body"`
	Error        string    `db:"error"         json:"error"`
	DeliveredAt  time.Time `db:"delivered_at"  json:"delivered_at"`
}
