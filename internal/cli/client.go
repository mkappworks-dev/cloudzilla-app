// Package cli is the HTTP client behind the cz command. It must not import
// internal/store or internal/service; a test enforces that.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxBody = 32 << 20

type Client struct {
	Host  string
	Token string
	HTTP  *http.Client
}

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func NormalizeHost(h string) string {
	h = strings.TrimRight(strings.TrimSpace(h), "/")
	if h != "" && !strings.Contains(h, "://") {
		h = "https://" + h
	}
	return h
}

// Do never follows redirects: a bad token on a page route redirects to an HTML login, and the Authorization header must not travel further.
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader, header http.Header) (*Response, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("path %q must start with /", path)
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), NormalizeHost(c.Host)+path, body)
	if err != nil {
		return nil, fmt.Errorf("bad request: %s", oneLine(err.Error()))
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cz")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	hc2 := *hc
	hc2.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	res, err := hc2.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %s", NormalizeHost(c.Host), oneLine(unwrapURLError(err)))
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("reading response: %s", oneLine(err.Error()))
	}
	resp := &Response{Status: res.StatusCode, Header: res.Header, Body: data}
	if res.StatusCode >= 300 {
		return resp, mapStatus(res, data)
	}
	return resp, nil
}

func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	resp, err := c.Do(ctx, "GET", "/api/user", nil, nil)
	if err != nil {
		return User{}, err
	}
	var u User
	if err := json.Unmarshal(resp.Body, &u); err != nil || u.Username == "" {
		return User{}, errors.New("unexpected response from /api/user; is this a Cloudzilla host?")
	}
	return u, nil
}

var scopeParam = regexp.MustCompile(`scope="([^"]*)"`)

func mapStatus(res *http.Response, body []byte) error {
	status := res.StatusCode
	switch {
	case status >= 300 && status < 400:
		return &Error{status, fmt.Sprintf("unexpected redirect (%d) to %s", status, oneLine(res.Header.Get("Location")))}
	case status == http.StatusUnauthorized:
		return &Error{status, "unauthorized: token missing, invalid or expired; run `cz auth login`"}
	case status == http.StatusForbidden && strings.Contains(res.Header.Get("WWW-Authenticate"), "insufficient_scope"):
		if m := scopeParam.FindStringSubmatch(res.Header.Get("WWW-Authenticate")); m != nil && m[1] != "" {
			return &Error{status, fmt.Sprintf("forbidden: token lacks scope %q; create a token with it", m[1])}
		}
		return &Error{status, "forbidden: no token scope allows this request"}
	case status == http.StatusTooManyRequests:
		msg := "rate limited"
		if d, ok := retryAfter(res.Header.Get("Retry-After")); ok {
			msg += fmt.Sprintf("; retry in %s", d.Round(time.Second))
		}
		return &Error{status, msg}
	}
	return &Error{status, fmt.Sprintf("HTTP %d: %s", status, errorText(res, body))}
}

func retryAfter(v string) (time.Duration, bool) {
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0), true
	}
	return 0, false
}

func errorText(res *http.Response, body []byte) string {
	var j struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &j) == nil && j.Error != "" {
		return oneLine(j.Error)
	}
	if t := oneLine(string(body)); t != "" && !strings.HasPrefix(t, "<") {
		return truncate(t, 200)
	}
	return strings.ToLower(http.StatusText(res.StatusCode))
}

func unwrapURLError(err error) string {
	var ue interface{ Unwrap() error }
	if errors.As(err, &ue) && ue.Unwrap() != nil {
		return ue.Unwrap().Error()
	}
	return err.Error()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
