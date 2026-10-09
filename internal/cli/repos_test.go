package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListReposFiltersByOwnerAndKeepsRawJSON(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/repos/" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"owner_name":"ada","name":"a","extra":1},{"owner_name":"Acme","name":"b","private":true}]`)
	})
	all, err := c.ListRepos(context.Background(), "")
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %v, %v", all, err)
	}
	got, err := c.ListRepos(context.Background(), "acme")
	if err != nil || len(got) != 1 || got[0].Name != "b" || !got[0].Private {
		t.Fatalf("filtered = %+v, %v", got, err)
	}
	if !strings.Contains(string(all[0].Raw), `"extra":1`) {
		t.Errorf("raw = %s", all[0].Raw)
	}
}

func TestGetRepoEscapesPath(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/repos/ada/demo" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		_, _ = io.WriteString(w, `{"owner_name":"ada","name":"demo","default_branch":"main"}`)
	})
	info, err := c.GetRepo(context.Background(), Repo{"ada", "demo"})
	if err != nil || info.DefaultBranch != "main" {
		t.Fatalf("%+v, %v", info, err)
	}
}

func TestCreateRepoPostsJSON(t *testing.T) {
	var path, ctype string
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, ctype = r.URL.Path, r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"owner_name":"ada","name":"demo"}`)
	})
	private := true
	info, err := c.CreateRepo(context.Background(), CreateRepo{Name: "demo", Description: "d", Private: &private, AddReadme: true, Gitignore: "Go", License: "mit"})
	if err != nil || info.Name != "demo" {
		t.Fatalf("%+v, %v", info, err)
	}
	if path != "/api/repos/" || ctype != "application/json" {
		t.Errorf("path = %q, content-type = %q", path, ctype)
	}
	want := map[string]any{"name": "demo", "description": "d", "private": true, "add_readme": true, "gitignore": "Go", "license": "mit"}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, body[k], v)
		}
	}
}

func TestCreateRepoInOrgOmitsUnsetPrivate(t *testing.T) {
	var path string
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"owner_name":"acme","name":"demo"}`)
	})
	if _, err := c.CreateRepo(context.Background(), CreateRepo{Org: "acme", Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	if path != "/api/orgs/acme/repos" {
		t.Errorf("path = %q", path)
	}
	if _, ok := body["private"]; ok {
		t.Error("private sent although unset; the org default would be overridden")
	}
}

func TestCreateRepoDuplicateNameShowsServerMessage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error":"a repository with that name already exists"}`)
	})
	_, err := c.CreateRepo(context.Background(), CreateRepo{Name: "demo"})
	if err == nil || !strings.Contains(err.Error(), "a repository with that name already exists") {
		t.Errorf("err = %v", err)
	}
}

func TestForkRepoSendsJSONBodySoTheServerAnswersJSON(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/repos/ada/demo/fork" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"owner":"bob","name":"demo","url":"/bob/demo"}`)
	})
	got, raw, err := c.ForkRepo(context.Background(), Repo{"ada", "demo"})
	if err != nil || got != (Repo{"bob", "demo"}) || !strings.Contains(string(raw), "/bob/demo") {
		t.Fatalf("%v, %s, %v", got, raw, err)
	}
}
