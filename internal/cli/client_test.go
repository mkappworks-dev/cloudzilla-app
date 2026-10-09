package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{Host: srv.URL, Token: "czp_secret"}
}

func TestClientSendsBearerAndReturnsBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer czp_secret" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("HX-Request") != "" {
			t.Error("HX-Request must not be set")
		}
		if r.URL.Path != "/api/repos/" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[]`)
	})
	resp, err := c.Do(context.Background(), "GET", "/api/repos/", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "[]" {
		t.Errorf("body = %q", resp.Body)
	}
}

func TestClientErrorMapping(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    []string
	}{
		{"401", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
		}, []string{"cz auth login"}},
		{"403 scope", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="repo:write"`)
			w.WriteHeader(403)
			_, _ = io.WriteString(w, `{"error":"insufficient_scope"}`)
		}, []string{"repo:write"}},
		{"403 no scope", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope"`)
			w.WriteHeader(403)
		}, []string{"no token scope"}},
		{"429", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "42")
			w.WriteHeader(429)
		}, []string{"42s"}},
		{"json error", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":"repo not found"}`)
		}, []string{"404", "repo not found"}},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		}, []string{"redirect", "/login"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, tt.handler)
			_, err := c.Do(context.Background(), "GET", "/api/x", nil, nil)
			if err == nil {
				t.Fatal("want error")
			}
			msg := err.Error()
			if strings.Contains(msg, "\n") {
				t.Errorf("multi-line error: %q", msg)
			}
			if strings.Contains(msg, "czp_secret") {
				t.Errorf("error leaks token: %q", msg)
			}
			for _, w := range tt.want {
				if !strings.Contains(msg, w) {
					t.Errorf("error %q lacks %q", msg, w)
				}
			}
		})
	}
}

func TestClientNetworkFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	host := srv.URL
	srv.Close()
	c := &Client{Host: host, Token: "czp_secret"}
	_, err := c.Do(context.Background(), "GET", "/api/user", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot reach") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("err = %v", err)
	}
}

func TestCurrentUser(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":7,"username":"ada"}`)
	})
	u, err := c.CurrentUser(context.Background())
	if err != nil || u.ID != 7 || u.Username != "ada" {
		t.Fatalf("u=%+v err=%v", u, err)
	}
}

func TestNormalizeHost(t *testing.T) {
	for in, want := range map[string]string{
		"git.example.com":          "https://git.example.com",
		"https://git.example.com/": "https://git.example.com",
		"http://localhost:8080":    "http://localhost:8080",
	} {
		if got := NormalizeHost(in); got != want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolvePrefersEnv(t *testing.T) {
	st := &Store{Keyring: newFakeKeyring(), Path: t.TempDir() + "/c.json"}
	_, _ = st.Save(Credentials{Host: "https://stored", Token: "stored-tok"})
	env := func(k string) string {
		return map[string]string{"CZ_HOST": "env.example", "CZ_TOKEN": "env-tok"}[k]
	}
	cr, src, err := Resolve(env, st)
	if err != nil || cr.Host != "https://env.example" || cr.Token != "env-tok" || src != SourceEnv {
		t.Fatalf("cr=%+v src=%q err=%v", cr, src, err)
	}
	cr, src, err = Resolve(func(string) string { return "" }, st)
	if err != nil || cr.Token != "stored-tok" || src == SourceEnv {
		t.Fatalf("cr=%+v src=%q err=%v", cr, src, err)
	}
	empty := &Store{Keyring: newFakeKeyring(), Path: t.TempDir() + "/c.json"}
	if _, _, err := Resolve(func(string) string { return "" }, empty); err != ErrNotLoggedIn {
		t.Fatalf("err = %v", err)
	}
}
