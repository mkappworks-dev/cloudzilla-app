package service

import (
	"context"
	"strings"
	"testing"
)

func TestRepoReadme_ResolvesRelativeLinksAndImagesAgainstTheCodeTab(t *testing.T) {
	t.Parallel()
	r := newPullRepo(t)
	r.commit(t, "main", "README.md", strings.Join([]string{
		"[guide](docs/guide.md) [up](../../escape.md) [root](/LICENSE) [ext](https://example.com/x) [frag](#usage) [q](a.md?plain=1#top)",
		"",
		"![logo](img/logo.png) ![abs](/assets/b.png) ![ext](https://example.com/i.png)",
	}, "\n"))

	html, name := r.svc.RepoReadme(context.Background(), "alice", "pulls", "main")

	if name != "README.md" {
		t.Errorf("name = %q, want README.md", name)
	}
	for _, want := range []string{
		`href="/alice/pulls/blob/main/docs/guide.md"`,
		`href="/alice/pulls/blob/main/escape.md"`,
		`href="/alice/pulls/blob/main/LICENSE"`,
		`href="https://example.com/x"`,
		`href="#usage"`,
		`href="/alice/pulls/blob/main/a.md?plain=1#top"`,
		`src="/alice/pulls/raw/main/img/logo.png"`,
		`src="/alice/pulls/raw/main/assets/b.png"`,
		`src="https://example.com/i.png"`,
	} {
		if !strings.Contains(string(html), want) {
			t.Errorf("README HTML lacks %s:\n%s", want, html)
		}
	}
}

func TestRepoReadme_FindsTheLowercaseNameAndAnEmptyRepoHasNone(t *testing.T) {
	t.Parallel()
	r := newPullRepo(t)
	if html, name := r.svc.RepoReadme(context.Background(), "alice", "pulls", "main"); html != "" || name != "" {
		t.Errorf("empty repo: got %q %q, want nothing", html, name)
	}
	r.commit(t, "main", "readme.md", "# hello\n")

	html, name := r.svc.RepoReadme(context.Background(), "alice", "pulls", "main")

	if name != "readme.md" || !strings.Contains(string(html), "hello") {
		t.Errorf("got %q %q, want readme.md rendered", html, name)
	}
}
