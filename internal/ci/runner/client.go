package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
)

// ErrUnauthorized means the server no longer accepts the runner's token: it was revoked.
var ErrUnauthorized = errors.New("the server rejected the runner token (revoked?)")

// Client speaks the runner protocol. Every call is an outbound request: the server never dials the runner.
type Client struct {
	Server string
	Token  string
	HTTP   *http.Client
}

func (c *Client) do(ctx context.Context, path, token string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Server, "/")+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: (runnerapi.MaxClaimWaitSeconds + 30) * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return resp.StatusCode, ErrUnauthorized
	case resp.StatusCode >= 300 && resp.StatusCode != http.StatusNoContent:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, fmt.Errorf("%s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("%s: decode response: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}

// Register trades a one-time registration token for the runner's own.
func Register(ctx context.Context, server, regToken string, req runnerapi.RegisterRequest) (*runnerapi.RegisterResponse, error) {
	var out runnerapi.RegisterResponse
	if _, err := (&Client{Server: server}).do(ctx, "/api/runner/register", regToken, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Claim long-polls for a Job and returns nil when none came within wait.
func (c *Client) Claim(ctx context.Context, labels []string, wait time.Duration) (*runnerapi.JobSpec, error) {
	var spec runnerapi.JobSpec
	code, err := c.do(ctx, fmt.Sprintf("/api/runner/claim?wait=%d", int(wait.Seconds())), c.Token, runnerapi.ClaimRequest{Labels: labels}, &spec)
	if err != nil || code == http.StatusNoContent {
		return nil, err
	}
	return &spec, nil
}

// Heartbeat renews the Job's lease and reports whether to carry on.
func (c *Client) Heartbeat(ctx context.Context, jobID int64) (bool, error) {
	var out runnerapi.HeartbeatResponse
	if _, err := c.do(ctx, fmt.Sprintf("/api/runner/jobs/%d/heartbeat", jobID), c.Token, nil, &out); err != nil {
		return false, err
	}
	return out.Continue, nil
}

func (c *Client) SetStep(ctx context.Context, jobID int64, position int, status string) error {
	_, err := c.do(ctx, fmt.Sprintf("/api/runner/jobs/%d/steps/%d", jobID, position), c.Token, runnerapi.StepRequest{Status: status}, nil)
	return err
}

func (c *Client) Finish(ctx context.Context, jobID int64, result, message string) error {
	_, err := c.do(ctx, fmt.Sprintf("/api/runner/jobs/%d/finish", jobID), c.Token, runnerapi.FinishRequest{Result: result, Message: message}, nil)
	return err
}
