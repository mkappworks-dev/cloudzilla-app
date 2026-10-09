package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

var (
	ErrDeviceFlowUnsupported = errors.New("this server doesn't support browser login; use `cz auth login --with-token`")
	ErrDeviceDenied          = errors.New("login denied in the browser")
	ErrDeviceExpired         = errors.New("the login code expired; run `cz auth login` again")
	ErrDeviceInvalidGrant    = errors.New("the login code is no longer valid; run `cz auth login` again")
)

// deviceScopes is what the browser flow may request; repo:admin is deliberately absent
// because the server refuses it for device logins.
var deviceScopes = []string{"repo:read", "repo:write", "issues:write", "pulls:write"}

const defaultDeviceScope = "repo:write"

// ValidateDeviceScopes splits space-separated values, drops duplicates and refuses
// anything the server would reject, so the user sees why before any request.
func ValidateDeviceScopes(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		for _, sc := range strings.Fields(s) {
			if seen[sc] {
				continue
			}
			seen[sc] = true
			if sc == "repo:admin" {
				return nil, errors.New(`scope "repo:admin" can't be requested by browser login; create a token under Settings, Access tokens, and use --with-token`)
			}
			if !contains(deviceScopes, sc) {
				return nil, fmt.Errorf("unknown scope %q; choose from %s", oneLine(sc), strings.Join(deviceScopes, ", "))
			}
			out = append(out, sc)
		}
	}
	if len(out) == 0 {
		out = []string{defaultDeviceScope}
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// Clock lets tests run the poll loop without waiting.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) RequestDeviceCode(ctx context.Context, scopes []string, deviceName string) (*DeviceCode, error) {
	form := url.Values{"scope": {strings.Join(scopes, " ")}}
	if deviceName != "" {
		form.Set("device_name", deviceName)
	}
	res, body, err := c.postForm(ctx, "/api/auth/device/code", form)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrDeviceFlowUnsupported
	}
	if res.StatusCode != http.StatusOK {
		return nil, mapStatus(res, body)
	}
	var dc DeviceCode
	if err := json.Unmarshal(body, &dc); err != nil || dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		return nil, errors.New("unexpected response from the device login endpoint; is this a Cloudzilla host?")
	}
	return &dc, nil
}

// PollDeviceToken sleeps after each response, never on a fixed ticker: the server enforces
// the interval from the previous poll and answers an early one with slow_down.
func (c *Client) PollDeviceToken(ctx context.Context, dc *DeviceCode, clk Clock) (string, error) {
	interval := time.Duration(dc.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := clk.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	wait := interval
	for {
		if err := clk.Sleep(ctx, wait); err != nil {
			return "", err
		}
		wait = interval
		if !clk.Now().Before(deadline) {
			return "", ErrDeviceExpired
		}
		res, body, err := c.postForm(ctx, "/api/auth/device/token", url.Values{
			"grant_type":  {deviceGrantType},
			"device_code": {dc.DeviceCode},
		})
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", err
		}
		if res.StatusCode == http.StatusOK {
			var ok struct {
				AccessToken string `json:"access_token"`
			}
			if json.Unmarshal(body, &ok) != nil || ok.AccessToken == "" {
				return "", errors.New("unexpected response from the device login endpoint; is this a Cloudzilla host?")
			}
			return ok.AccessToken, nil
		}
		if res.StatusCode == http.StatusTooManyRequests {
			if d, ok := retryAfter(res.Header.Get("Retry-After")); ok {
				wait = max(d, interval)
			}
			continue
		}
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		switch e.Error {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
			wait = interval
		case "access_denied":
			return "", ErrDeviceDenied
		case "expired_token":
			return "", ErrDeviceExpired
		case "invalid_grant":
			return "", ErrDeviceInvalidGrant
		default:
			return "", mapStatus(res, body)
		}
	}
}

// postForm skips Client.Do: the device endpoints are unauthenticated, and Do always sends a Bearer header.
func (c *Client) postForm(ctx context.Context, path string, form url.Values) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, NormalizeHost(c.Host)+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, fmt.Errorf("bad request: %s", oneLine(err.Error()))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cz")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	hc2 := *hc
	hc2.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := hc2.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot reach %s: %s", NormalizeHost(c.Host), oneLine(unwrapURLError(err)))
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("reading response: %s", oneLine(err.Error()))
	}
	return res, data, nil
}
