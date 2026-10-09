package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Issue holds the fields the tables show; Raw is the server's JSON for --json.
type Issue struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	State      string    `json:"state"`
	AuthorName string    `json:"author_name"`
	CreatedAt  time.Time `json:"created_at"`

	Raw json.RawMessage `json:"-"`
}

type IssueComment struct {
	AuthorName string    `json:"author_name"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`

	Raw json.RawMessage `json:"-"`
}

type Label struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PartialIssueError reports a create that succeeded but whose label or assignee step did not.
type PartialIssueError struct {
	Issue Issue
	Step  string
	Err   error
}

func (e *PartialIssueError) Error() string {
	return fmt.Sprintf("issue #%d was created, but %s failed: %s", e.Issue.Number, e.Step, e.Err)
}

func (e *PartialIssueError) Unwrap() error { return e.Err }

func issuePath(r Repo, number int) string {
	return repoPath(r) + "/issues/" + strconv.Itoa(number)
}

// ListIssues filters by state on the client: GET …/issues/ has no state or label parameter and returns at most 500 issues.
func (c *Client) ListIssues(ctx context.Context, r Repo, state string) ([]Issue, error) {
	resp, err := c.Do(ctx, "GET", repoPath(r)+"/issues/", nil, nil)
	if err != nil {
		return nil, err
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(resp.Body, &raws); err != nil {
		return nil, errors.New("unexpected response listing issues; is this a Cloudzilla host?")
	}
	out := make([]Issue, 0, len(raws))
	for _, raw := range raws {
		is, err := decodeIssue(raw)
		if err != nil {
			return nil, err
		}
		if state == "" || is.State == state {
			out = append(out, is)
		}
	}
	return out, nil
}

func (c *Client) GetIssue(ctx context.Context, r Repo, number int) (Issue, error) {
	resp, err := c.Do(ctx, "GET", issuePath(r, number), nil, nil)
	if err != nil {
		return Issue{}, err
	}
	return decodeIssue(resp.Body)
}

func (c *Client) ListIssueComments(ctx context.Context, r Repo, number int) ([]IssueComment, error) {
	resp, err := c.Do(ctx, "GET", issuePath(r, number)+"/comments", nil, nil)
	if err != nil {
		return nil, err
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(resp.Body, &raws); err != nil {
		return nil, errors.New("unexpected response listing comments")
	}
	out := make([]IssueComment, 0, len(raws))
	for _, raw := range raws {
		var cm IssueComment
		if err := json.Unmarshal(raw, &cm); err != nil {
			return nil, errors.New("unexpected comment in the response")
		}
		cm.Raw = raw
		out = append(out, cm)
	}
	return out, nil
}

func (c *Client) CreateIssue(ctx context.Context, r Repo, title, body string) (Issue, error) {
	data, err := json.Marshal(map[string]string{"title": title, "body": body})
	if err != nil {
		return Issue{}, err
	}
	resp, err := c.Do(ctx, "POST", repoPath(r)+"/issues/", bytes.NewReader(data), jsonHeader)
	if err != nil {
		return Issue{}, err
	}
	return decodeIssue(resp.Body)
}

func (c *Client) CommentOnIssue(ctx context.Context, r Repo, number int, body string) (IssueComment, error) {
	data, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return IssueComment{}, err
	}
	resp, err := c.Do(ctx, "POST", issuePath(r, number)+"/comments", bytes.NewReader(data), jsonHeader)
	if err != nil {
		return IssueComment{}, err
	}
	var cm IssueComment
	if err := json.Unmarshal(resp.Body, &cm); err != nil {
		return IssueComment{}, errors.New("unexpected comment in the response")
	}
	cm.Raw = resp.Body
	return cm, nil
}

func (c *Client) SetIssueState(ctx context.Context, r Repo, number int, state string) (Issue, error) {
	data, err := json.Marshal(map[string]string{"state": state})
	if err != nil {
		return Issue{}, err
	}
	resp, err := c.Do(ctx, "PATCH", issuePath(r, number), bytes.NewReader(data), jsonHeader)
	if err != nil {
		return Issue{}, err
	}
	return decodeIssue(resp.Body)
}

func (c *Client) ListLabels(ctx context.Context, r Repo) ([]Label, error) {
	resp, err := c.Do(ctx, "GET", repoPath(r)+"/labels/", nil, nil)
	if err != nil {
		return nil, err
	}
	var labels []Label
	if err := json.Unmarshal(resp.Body, &labels); err != nil {
		return nil, errors.New("unexpected response listing labels")
	}
	return labels, nil
}

// ResolveLabels maps names to IDs ahead of creating anything, so an unknown label leaves no half-made issue.
func (c *Client) ResolveLabels(ctx context.Context, r Repo, names []string) ([]int64, error) {
	if len(names) == 0 {
		return nil, nil
	}
	labels, err := c.ListLabels(ctx, r)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		id, ok := int64(0), false
		for _, l := range labels {
			if strings.EqualFold(l.Name, name) {
				id, ok = l.ID, true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("label %q does not exist in %s", name, r)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (c *Client) AddIssueLabel(ctx context.Context, r Repo, number int, labelID int64) error {
	_, err := c.Do(ctx, "POST", issuePath(r, number)+"/labels/"+strconv.FormatInt(labelID, 10), nil, nil)
	return err
}

func (c *Client) AddIssueAssignee(ctx context.Context, r Repo, number int, username string) error {
	data, err := json.Marshal(map[string]string{"username": username})
	if err != nil {
		return err
	}
	_, err = c.Do(ctx, "POST", issuePath(r, number)+"/assignees", bytes.NewReader(data), jsonHeader)
	return err
}

// CreateIssueWith creates the issue, then applies labels and assignees; a failure after creation returns a *PartialIssueError.
func (c *Client) CreateIssueWith(ctx context.Context, r Repo, title, body string, labels, assignees []string) (Issue, error) {
	ids, err := c.ResolveLabels(ctx, r, labels)
	if err != nil {
		return Issue{}, fmt.Errorf("%w (no issue was created)", err)
	}
	is, err := c.CreateIssue(ctx, r, title, body)
	if err != nil {
		return Issue{}, err
	}
	for i, id := range ids {
		if err := c.AddIssueLabel(ctx, r, is.Number, id); err != nil {
			return is, &PartialIssueError{is, fmt.Sprintf("adding label %q", labels[i]), err}
		}
	}
	for _, u := range assignees {
		if err := c.AddIssueAssignee(ctx, r, is.Number, u); err != nil {
			return is, &PartialIssueError{is, fmt.Sprintf("assigning %q", u), err}
		}
	}
	return is, nil
}

func decodeIssue(body []byte) (Issue, error) {
	var is Issue
	if err := json.Unmarshal(body, &is); err != nil || is.Number == 0 {
		return Issue{}, errors.New("unexpected issue in the response; is this a Cloudzilla host?")
	}
	is.Raw = append(json.RawMessage(nil), body...)
	return is, nil
}
