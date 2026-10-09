package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// RepoInfo holds the fields the tables show; Raw is the server's JSON for --json.
type RepoInfo struct {
	Owner         string `json:"owner_name"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	IsFork        bool   `json:"is_fork"`
	IsArchived    bool   `json:"is_archived"`
	ForkCount     int    `json:"fork_count"`
	UpdatedAt     string `json:"updated_at"`

	Raw json.RawMessage `json:"-"`
}

func (r RepoInfo) Repo() Repo { return Repo{r.Owner, r.Name} }

type CreateRepo struct {
	Org         string
	Name        string
	Description string
	// Nil leaves an organization's default visibility in place.
	Private   *bool
	AddReadme bool
	Gitignore string
	License   string
}

var jsonHeader = http.Header{"Content-Type": {"application/json"}}

func repoPath(r Repo) string {
	return "/api/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Name)
}

// ListRepos filters by owner on the client: GET /api/repos/ has no owner parameter.
func (c *Client) ListRepos(ctx context.Context, owner string) ([]RepoInfo, error) {
	resp, err := c.Do(ctx, "GET", "/api/repos/", nil, nil)
	if err != nil {
		return nil, err
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(resp.Body, &raws); err != nil {
		return nil, errors.New("unexpected response listing repositories; is this a Cloudzilla host?")
	}
	out := make([]RepoInfo, 0, len(raws))
	for _, raw := range raws {
		var info RepoInfo
		if err := json.Unmarshal(raw, &info); err != nil {
			return nil, errors.New("unexpected repository in the response")
		}
		if owner != "" && !strings.EqualFold(info.Owner, owner) {
			continue
		}
		info.Raw = raw
		out = append(out, info)
	}
	return out, nil
}

func (c *Client) GetRepo(ctx context.Context, r Repo) (RepoInfo, error) {
	resp, err := c.Do(ctx, "GET", repoPath(r), nil, nil)
	if err != nil {
		return RepoInfo{}, err
	}
	return decodeRepo(resp.Body)
}

func (c *Client) CreateRepo(ctx context.Context, p CreateRepo) (RepoInfo, error) {
	body := map[string]any{"name": p.Name, "description": p.Description}
	if p.Private != nil {
		body["private"] = *p.Private
	}
	if p.AddReadme {
		body["add_readme"] = true
	}
	if p.Gitignore != "" {
		body["gitignore"] = p.Gitignore
	}
	if p.License != "" {
		body["license"] = p.License
	}
	data, err := json.Marshal(body)
	if err != nil {
		return RepoInfo{}, err
	}
	path := "/api/repos/"
	if p.Org != "" {
		path = "/api/orgs/" + url.PathEscape(p.Org) + "/repos"
	}
	resp, err := c.Do(ctx, "POST", path, bytes.NewReader(data), jsonHeader)
	if err != nil {
		return RepoInfo{}, err
	}
	return decodeRepo(resp.Body)
}

// ForkRepo sends an empty JSON object because any other body makes the server answer with a redirect.
func (c *Client) ForkRepo(ctx context.Context, r Repo) (Repo, json.RawMessage, error) {
	resp, err := c.Do(ctx, "POST", repoPath(r)+"/fork", strings.NewReader("{}"), jsonHeader)
	if err != nil {
		return Repo{}, nil, err
	}
	var f struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(resp.Body, &f); err != nil || f.Owner == "" || f.Name == "" {
		return Repo{}, nil, errors.New("unexpected response forking the repository")
	}
	return Repo{f.Owner, f.Name}, resp.Body, nil
}

func decodeRepo(body []byte) (RepoInfo, error) {
	var info RepoInfo
	if err := json.Unmarshal(body, &info); err != nil || info.Name == "" {
		return RepoInfo{}, errors.New("unexpected repository in the response; is this a Cloudzilla host?")
	}
	info.Raw = body
	return info, nil
}
