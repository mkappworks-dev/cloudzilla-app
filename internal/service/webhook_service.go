package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

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

func (s *WebhookService) ListDeliveries(ctx context.Context, webhookID int64) ([]model.WebhookDelivery, error) {
	return s.webhooks.ListDeliveries(ctx, webhookID)
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
		if !strings.Contains(wh.Events, event) {
			continue
		}
		go s.deliver(wh, event, payloadBytes)
	}
}

func (s *WebhookService) deliver(wh model.Webhook, event string, payload []byte) {
	ctx := context.Background()
	d := &model.WebhookDelivery{
		WebhookID: wh.ID,
		Event:     event,
		Payload:   string(payload),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, strings.NewReader(string(payload)))
	if err != nil {
		d.Error = err.Error()
		_ = s.webhooks.LogDelivery(ctx, d)
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
		_ = s.webhooks.LogDelivery(ctx, d)
		return
	}
	defer resp.Body.Close()
	d.ResponseCode = resp.StatusCode
	_ = s.webhooks.LogDelivery(ctx, d)
}

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
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
