package cli

import (
	"os/exec"
	"strings"
	"testing"
)

func TestParseRemote(t *testing.T) {
	cases := []struct {
		remote, host string
		want         Repo
		errHas       string
	}{
		{"https://git.example.com/ada/demo.git", "https://git.example.com", Repo{"ada", "demo"}, ""},
		{"https://git.example.com/ada/demo", "https://git.example.com", Repo{"ada", "demo"}, ""},
		{"https://git.example.com/ada/demo/", "https://git.example.com", Repo{"ada", "demo"}, ""},
		{"https://ada:secret@GIT.example.com/ada/demo.git", "https://git.example.com", Repo{"ada", "demo"}, ""},
		{"https://git.example.com:443/ada/demo.git", "https://git.example.com", Repo{"ada", "demo"}, ""},
		{"http://localhost:8080/ada/demo.git", "http://localhost:8080", Repo{"ada", "demo"}, ""},
		{"git@git.example.com:ada/demo.git", "https://git.example.com", Repo{"ada", "demo"}, ""},
		{"ssh://git@git.example.com:2222/ada/demo.git", "https://git.example.com", Repo{"ada", "demo"}, ""},
		{"ssh://git@localhost:2222/ada/demo.git", "http://localhost:8080", Repo{"ada", "demo"}, ""},
		{"git@github.com:ada/demo.git", "https://git.example.com", Repo{}, "github.com, not git.example.com"},
		{"https://localhost:9000/ada/demo.git", "http://localhost:8080", Repo{}, "not localhost:8080"},
		{"https://git.example.com/ada", "https://git.example.com", Repo{}, "owner/repo"},
		{"https://git.example.com/a/b/c.git", "https://git.example.com", Repo{}, "owner/repo"},
		{"/srv/git/demo.git", "https://git.example.com", Repo{}, "not a URL"},
	}
	for _, c := range cases {
		got, err := ParseRemote(c.remote, c.host)
		if c.errHas != "" {
			if err == nil || !strings.Contains(err.Error(), c.errHas) {
				t.Errorf("ParseRemote(%q) err = %v, want containing %q", c.remote, err, c.errHas)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParseRemote(%q) = %v, %v; want %v", c.remote, got, err, c.want)
		}
	}
}

func TestParseRepo(t *testing.T) {
	host := "https://git.example.com"
	for in, want := range map[string]Repo{
		"ada/demo":                         {"ada", "demo"},
		"ada/demo.git":                     {"ada", "demo"},
		"git.example.com/ada/demo":         {"ada", "demo"},
		"https://git.example.com/ada/demo": {"ada", "demo"},
		"git@git.example.com:ada/demo.git": {"ada", "demo"},
	} {
		got, err := ParseRepo(in, host)
		if err != nil || got != want {
			t.Errorf("ParseRepo(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "demo", "a/b/c", "/demo", "ada/", "github.com/ada/demo", "https://github.com/ada/demo"} {
		if _, err := ParseRepo(in, host); err == nil {
			t.Errorf("ParseRepo(%q) accepted", in)
		}
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRepoFromRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	if _, err := RepoFromRemote(dir, "https://git.example.com"); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Errorf("no origin: err = %v", err)
	}
	gitIn(t, dir, "remote", "add", "origin", "git@git.example.com:ada/demo.git")
	got, err := RepoFromRemote(dir, "https://git.example.com")
	if err != nil || got != (Repo{"ada", "demo"}) {
		t.Errorf("got %v, %v", got, err)
	}
	if _, err := RepoFromRemote(dir, "https://other.example.org"); err == nil || !strings.Contains(err.Error(), "not other.example.org") {
		t.Errorf("foreign host: err = %v", err)
	}
	if _, err := RepoFromRemote(t.TempDir(), "https://git.example.com"); err == nil {
		t.Error("outside a checkout: want an error")
	}
}
