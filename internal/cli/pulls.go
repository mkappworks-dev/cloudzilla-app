package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

type Pull struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	State      string `json:"state"`
	HeadBranch string `json:"head_branch"`
	BaseBranch string `json:"base_branch"`
	AuthorName string `json:"author_name"`
	IsDraft    bool   `json:"is_draft"`
	CreatedAt  string `json:"created_at"`

	Raw json.RawMessage `json:"-"`
}

type Review struct {
	AuthorName string `json:"author_name"`
	State      string `json:"state"`
	Body       string `json:"body"`

	Raw json.RawMessage `json:"-"`
}

type LineComment struct {
	AuthorName string `json:"author_name"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Body       string `json:"body"`

	Raw json.RawMessage `json:"-"`
}

type CreatePull struct {
	Title string
	Body  string
	Head  string
	Base  string
	Draft bool
}

// Review states as the server stores them.
const (
	ReviewApproved         = "approved"
	ReviewChangesRequested = "changes_requested"
	ReviewCommented        = "commented"
)

func pullPath(r Repo, number int) string {
	return repoPath(r) + "/pulls/" + strconv.Itoa(number)
}

// ListPulls filters by state on the client: GET …/pulls/ has no state parameter and returns every state. An empty state or "all" keeps everything.
func (c *Client) ListPulls(ctx context.Context, r Repo, state string) ([]Pull, error) {
	raws, err := c.listRaw(ctx, repoPath(r)+"/pulls/")
	if err != nil {
		return nil, err
	}
	out := make([]Pull, 0, len(raws))
	for _, raw := range raws {
		p, err := decodePull(raw)
		if err != nil {
			return nil, err
		}
		if state != "" && state != "all" && p.State != state {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (c *Client) GetPull(ctx context.Context, r Repo, number int) (Pull, error) {
	resp, err := c.Do(ctx, "GET", pullPath(r, number), nil, nil)
	if err != nil {
		return Pull{}, err
	}
	return decodePull(resp.Body)
}

func (c *Client) ListReviews(ctx context.Context, r Repo, number int) ([]Review, error) {
	raws, err := c.listRaw(ctx, pullPath(r, number)+"/reviews")
	if err != nil {
		return nil, err
	}
	out := make([]Review, 0, len(raws))
	for _, raw := range raws {
		var v Review
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, errors.New("unexpected review in the response")
		}
		v.Raw = raw
		out = append(out, v)
	}
	return out, nil
}

func (c *Client) ListLineComments(ctx context.Context, r Repo, number int) ([]LineComment, error) {
	raws, err := c.listRaw(ctx, pullPath(r, number)+"/line_comments")
	if err != nil {
		return nil, err
	}
	out := make([]LineComment, 0, len(raws))
	for _, raw := range raws {
		var v LineComment
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, errors.New("unexpected line comment in the response")
		}
		v.Raw = raw
		out = append(out, v)
	}
	return out, nil
}

// The server marshals an empty list as null, which unmarshals to an empty slice here.
func (c *Client) listRaw(ctx context.Context, path string) ([]json.RawMessage, error) {
	resp, err := c.Do(ctx, "GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(resp.Body, &raws); err != nil {
		return nil, errors.New("unexpected response; is this a Cloudzilla host?")
	}
	return raws, nil
}

func (c *Client) CreatePull(ctx context.Context, r Repo, p CreatePull) (Pull, error) {
	data, err := json.Marshal(map[string]any{
		"title": p.Title, "body": p.Body, "head_branch": p.Head, "base_branch": p.Base, "is_draft": p.Draft,
	})
	if err != nil {
		return Pull{}, err
	}
	resp, err := c.Do(ctx, "POST", repoPath(r)+"/pulls/", bytes.NewReader(data), jsonHeader)
	if err != nil {
		return Pull{}, err
	}
	return decodePull(resp.Body)
}

// The server's 403 for a pulls:write token only names the scope, so say why it is needed.
func (c *Client) MergePull(ctx context.Context, r Repo, number int, strategy string) (Pull, error) {
	p, err := c.patchPull(ctx, r, number, map[string]any{"state": "merged", "merge_strategy": strategy})
	var e *Error
	if errors.As(err, &e) && e.Status == http.StatusForbidden && strings.Contains(e.Msg, `"repo:write"`) {
		return Pull{}, &Error{e.Status, `forbidden: merging needs a token with the repo:write scope; pulls:write is not enough`}
	}
	return p, err
}

// ClosePull refuses a pull request that is no longer open: the server would rewrite a merged one as closed.
func (c *Client) ClosePull(ctx context.Context, r Repo, number int) (Pull, error) {
	cur, err := c.GetPull(ctx, r, number)
	if err != nil {
		return Pull{}, err
	}
	if cur.State != "open" {
		return Pull{}, fmt.Errorf("#%d is already %s", number, cur.State)
	}
	return c.patchPull(ctx, r, number, map[string]any{"state": "closed"})
}

func (c *Client) patchPull(ctx context.Context, r Repo, number int, body map[string]any) (Pull, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return Pull{}, err
	}
	resp, err := c.Do(ctx, "PATCH", pullPath(r, number), bytes.NewReader(data), jsonHeader)
	if err != nil {
		return Pull{}, err
	}
	return decodePull(resp.Body)
}

func (c *Client) SubmitReview(ctx context.Context, r Repo, number int, state, body string) (Review, error) {
	data, err := json.Marshal(map[string]string{"state": state, "body": body})
	if err != nil {
		return Review{}, err
	}
	resp, err := c.Do(ctx, "POST", pullPath(r, number)+"/reviews", bytes.NewReader(data), jsonHeader)
	if err != nil {
		return Review{}, err
	}
	var v Review
	if err := json.Unmarshal(resp.Body, &v); err != nil {
		return Review{}, errors.New("unexpected response submitting the review")
	}
	v.Raw = resp.Body
	return v, nil
}

func decodePull(body []byte) (Pull, error) {
	var p Pull
	if err := json.Unmarshal(body, &p); err != nil || p.Number == 0 {
		return Pull{}, errors.New("unexpected pull request in the response; is this a Cloudzilla host?")
	}
	p.Raw = body
	return p, nil
}

// CurrentBranch names the branch checked out in dir.
func CurrentBranch(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "branch", "--show-current")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("git is not installed; pass --head")
		}
		if strings.Contains(stderr.String(), "not a git repository") {
			return "", errors.New("not inside a git checkout; pass --head")
		}
		return "", fmt.Errorf("reading the current branch: %s", oneLine(stderr.String()))
	}
	b := strings.TrimSpace(string(out))
	if b == "" {
		return "", errors.New("HEAD is detached; pass --head")
	}
	return b, nil
}
