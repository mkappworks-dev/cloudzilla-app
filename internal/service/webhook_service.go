package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

// isInternalURL checks if a URL targets a private/internal IP range.
func isInternalURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return true // reject unparseable or hostless URLs
	}
	host := u.Hostname()

	// Block common internal hostnames
	if host == "localhost" || host == "metadata.google.internal" {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		// Could be a hostname that resolves to internal IP — resolve it
		addrs, err := net.LookupHost(host)
		if err != nil || len(addrs) == 0 {
			return false // let it fail naturally
		}
		ip = net.ParseIP(addrs[0])
		if ip == nil {
			return false
		}
	}

	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

type WebhookService struct {
	webhooks *store.WebhookStore
	client   *http.Client
}

func NewWebhookService(webhooks *store.WebhookStore) *WebhookService {
	return &WebhookService{
		webhooks: webhooks,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *WebhookService) Create(ctx context.Context, repoID int64, url, secret, events string) (*model.Webhook, error) {
	if isInternalURL(url) {
		return nil, fmt.Errorf("webhook URL must not target internal networks")
	}
	if events == "" {
		events = "push,issues,pull_request"
	}
	wh := &model.Webhook{
		RepoID: repoID,
		URL:    url,
		Secret: secret,
		Events: events,
		Active: true,
	}
	if err := s.webhooks.Create(ctx, wh); err != nil {
		return nil, fmt.Errorf("webhook create: %w", err)
	}
	return wh, nil
}

func (s *WebhookService) ListByRepo(ctx context.Context, repoID int64) ([]model.Webhook, error) {
	return s.webhooks.ListByRepo(ctx, repoID)
}

func (s *WebhookService) Delete(ctx context.Context, id, repoID int64) error {
	return s.webhooks.Delete(ctx, id, repoID)
}

func (s *WebhookService) ListDeliveries(ctx context.Context, webhookID, repoID int64) ([]model.WebhookDelivery, error) {
	wh, err := s.webhooks.GetByID(ctx, webhookID)
	if err != nil {
		return nil, fmt.Errorf("webhook not found: %w", err)
	}
	if wh.RepoID != repoID {
		return nil, fmt.Errorf("forbidden")
	}
	return s.webhooks.ListDeliveriesWithRetry(ctx, webhookID)
}

// Dispatch is fire-and-forget; call as `go s.Dispatch(...)`.
func (s *WebhookService) Dispatch(repoID int64, event string, payload any) {
	ctx := context.Background()
	hooks, err := s.webhooks.ListByRepo(ctx, repoID)
	if err != nil {
		return
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for _, wh := range hooks {
		if !wh.Active {
			continue
		}
		if !strings.Contains(","+wh.Events+",", ","+event+",") {
			continue
		}
		go s.deliver(wh, event, payloadBytes)
	}
}

func (s *WebhookService) deliver(wh model.Webhook, event string, payload []byte) {
	if isInternalURL(wh.URL) {
		slog.Warn("webhook: blocked delivery to internal URL", "webhook_id", wh.ID, "url", wh.URL)
		return
	}

	ctx := context.Background()
	d := &model.WebhookDelivery{
		WebhookID: wh.ID,
		Event:     event,
		Payload:   string(payload),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, strings.NewReader(string(payload)))
	if err != nil {
		d.Error = err.Error()
		if logErr := s.webhooks.LogDelivery(ctx, d); logErr == nil {
			t := time.Now().Add(webhookBackoff(1))
			if retryErr := s.webhooks.UpdateDeliveryRetry(ctx, d.ID, &t, 1, 0, d.Error); retryErr != nil {
				slog.Warn("webhook: failed to schedule retry", "delivery_id", d.ID, "error", retryErr)
			}
		}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cloudzilla-Event", event)
	if wh.Secret != "" {
		req.Header.Set("X-Hub-Signature-256", "sha256="+computeHMAC(payload, wh.Secret))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		d.Error = err.Error()
		if logErr := s.webhooks.LogDelivery(ctx, d); logErr == nil {
			t := time.Now().Add(webhookBackoff(1))
			if retryErr := s.webhooks.UpdateDeliveryRetry(ctx, d.ID, &t, 1, 0, d.Error); retryErr != nil {
				slog.Warn("webhook: failed to schedule retry", "delivery_id", d.ID, "error", retryErr)
			}
		}
		return
	}
	defer resp.Body.Close()
	d.ResponseCode = resp.StatusCode
	if logErr := s.webhooks.LogDelivery(ctx, d); logErr != nil {
		return
	}
	// Schedule retry on non-2xx
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t := time.Now().Add(webhookBackoff(1))
		if retryErr := s.webhooks.UpdateDeliveryRetry(ctx, d.ID, &t, 1, resp.StatusCode, ""); retryErr != nil {
			slog.Warn("webhook: failed to schedule retry", "delivery_id", d.ID, "error", retryErr)
		}
	}
}

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// webhookBackoff returns the delay before the next retry for a given attempt number.
// Attempt 1 → 1 min, 2 → 5 min, 3 → 30 min, 4 → 2 h, 5+ → 8 h.
func webhookBackoff(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 1 * time.Minute
	case attempt == 2:
		return 5 * time.Minute
	case attempt == 3:
		return 30 * time.Minute
	case attempt == 4:
		return 2 * time.Hour
	default:
		return 8 * time.Hour
	}
}

// RetryPending finds all due webhook deliveries and re-delivers them.
// Call this from a background goroutine on a ticker.
func (s *WebhookService) RetryPending(ctx context.Context) error {
	pending, err := s.webhooks.ListPendingRetry(ctx)
	if err != nil {
		return fmt.Errorf("retry pending list: %w", err)
	}
	for _, d := range pending {
		wh, err := s.webhooks.GetByID(ctx, d.WebhookID)
		if err != nil {
			// Webhook deleted; clear retry
			_ = s.webhooks.UpdateDeliveryRetry(ctx, d.ID, nil, d.AttemptCount, d.ResponseCode, "webhook deleted")
			continue
		}
		go s.retryDeliver(ctx, *wh, d)
	}
	return nil
}

func (s *WebhookService) retryDeliver(ctx context.Context, wh model.Webhook, d model.WebhookDelivery) {
	newAttempt := d.AttemptCount + 1

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, strings.NewReader(d.Payload))
	if err != nil {
		var nextRetry *time.Time
		if newAttempt < 5 {
			t := time.Now().Add(webhookBackoff(newAttempt))
			nextRetry = &t
		}
		if retryErr := s.webhooks.UpdateDeliveryRetry(ctx, d.ID, nextRetry, newAttempt, 0, err.Error()); retryErr != nil {
			slog.Warn("webhook: failed to update retry state", "delivery_id", d.ID, "error", retryErr)
		}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cloudzilla-Event", d.Event)
	if wh.Secret != "" {
		req.Header.Set("X-Hub-Signature-256", "sha256="+computeHMAC([]byte(d.Payload), wh.Secret))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		var nextRetry *time.Time
		if newAttempt < 5 {
			t := time.Now().Add(webhookBackoff(newAttempt))
			nextRetry = &t
		}
		if retryErr := s.webhooks.UpdateDeliveryRetry(ctx, d.ID, nextRetry, newAttempt, 0, err.Error()); retryErr != nil {
			slog.Warn("webhook: failed to update retry state", "delivery_id", d.ID, "error", retryErr)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if retryErr := s.webhooks.UpdateDeliveryRetry(ctx, d.ID, nil, newAttempt, resp.StatusCode, ""); retryErr != nil {
			slog.Warn("webhook: failed to clear retry state", "delivery_id", d.ID, "error", retryErr)
		}
		return
	}

	// Non-2xx — schedule next retry if under limit
	var nextRetry *time.Time
	if newAttempt < 5 {
		t := time.Now().Add(webhookBackoff(newAttempt))
		nextRetry = &t
	}
	if retryErr := s.webhooks.UpdateDeliveryRetry(ctx, d.ID, nextRetry, newAttempt, resp.StatusCode, ""); retryErr != nil {
		slog.Warn("webhook: failed to update retry state", "delivery_id", d.ID, "error", retryErr)
	}
}

// RedeliverByID re-sends a specific delivery immediately (manual redeliver).
func (s *WebhookService) RedeliverByID(ctx context.Context, deliveryID, repoID int64) error {
	d, err := s.webhooks.GetDeliveryByID(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("delivery not found: %w", err)
	}
	wh, err := s.webhooks.GetByID(ctx, d.WebhookID)
	if err != nil {
		return fmt.Errorf("webhook not found: %w", err)
	}
	if wh.RepoID != repoID {
		return fmt.Errorf("forbidden")
	}
	go s.retryDeliver(context.Background(), *wh, *d)
	return nil
}

// UpdateEvents updates the event filter list for a webhook.
// events is a comma-separated string, e.g. "push,issues".
func (s *WebhookService) UpdateEvents(ctx context.Context, webhookID, repoID int64, events string) error {
	if events == "" {
		return fmt.Errorf("events must not be empty")
	}
	return s.webhooks.UpdateWebhookEvents(ctx, webhookID, repoID, events)
}

func (s *WebhookService) PushPayload(repo model.Repository, pusher, branch, headSHA string) map[string]any {
	return map[string]any{
		"ref":    "refs/heads/" + branch,
		"after":  headSHA,
		"pusher": map[string]any{"name": pusher},
		"repository": map[string]any{
			"id":   repo.ID,
			"name": repo.Name,
		},
	}
}

func (s *WebhookService) IssuePayload(action string, repo model.Repository, issue model.Issue) map[string]any {
	return map[string]any{
		"action": action,
		"issue": map[string]any{
			"id":     issue.ID,
			"number": issue.Number,
			"title":  issue.Title,
			"state":  string(issue.State),
		},
		"repository": map[string]any{
			"id":   repo.ID,
			"name": repo.Name,
		},
	}
}

func (s *WebhookService) PullPayload(action string, repo model.Repository, pr model.PullRequest) map[string]any {
	return map[string]any{
		"action": action,
		"pull_request": map[string]any{
			"id":          pr.ID,
			"number":      pr.Number,
			"title":       pr.Title,
			"state":       string(pr.State),
			"head_branch": pr.HeadBranch,
			"base_branch": pr.BaseBranch,
		},
		"repository": map[string]any{
			"id":   repo.ID,
			"name": repo.Name,
		},
	}
}

func (s *WebhookService) ReleasePayload(action, owner, repoName string, release model.Release) map[string]any {
	return map[string]any{
		"action": action,
		"release": map[string]any{
			"id":            release.ID,
			"tag_name":      release.TagName,
			"name":          release.Name,
			"is_prerelease": release.IsPrerelease,
			"is_draft":      release.IsDraft,
		},
		"repository": map[string]any{
			"id":        release.RepoID,
			"name":      repoName,
			"full_name": owner + "/" + repoName,
		},
	}
}
