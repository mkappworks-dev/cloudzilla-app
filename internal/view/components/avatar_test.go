package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderString(t *testing.T, ctx context.Context, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

const testKey = "avatars/user/7/" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.png"

func TestAvatar_UsesKeyFromContext(t *testing.T) {
	ctx := WithAvatarKeys(context.Background(), map[string]string{"alice": testKey})
	out := renderString(t, ctx, Avatar("alice", AvatarSizeSM, "alice"))
	for _, want := range []string{`<img`, `data-avatar`, `src="/` + testKey + `"`, `aria-label="alice"`, `width="24"`, `loading="lazy"`, ">A</span>"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in %s", want, out)
		}
	}
	if out := renderString(t, ctx, Avatar("bob", AvatarSizeSM, "bob")); strings.Contains(out, "<img") {
		t.Errorf("a name without a key rendered an image: %s", out)
	}
}

// A git author name can equal an unrelated username, so Initials must never
// pick up that user's avatar.
func TestInitials_IgnoresAvatarKeys(t *testing.T) {
	ctx := WithAvatarKeys(context.Background(), map[string]string{"alice": testKey})
	out := renderString(t, ctx, Initials("alice", AvatarSizeSM, "alice"))
	if strings.Contains(out, "<img") || !strings.Contains(out, `aria-label="alice"`) {
		t.Errorf("got %s", out)
	}
}

func TestWithAvatarKeys_MergesWithEarlierKeys(t *testing.T) {
	ctx := WithAvatarKeys(context.Background(), map[string]string{"alice": testKey})
	ctx = WithAvatarKeys(ctx, map[string]string{"acme": "avatars/org/1/x.png"})
	if avatarKeyFor(ctx, "alice") != testKey || avatarKeyFor(ctx, "acme") == "" {
		t.Error("a later WithAvatarKeys dropped earlier keys")
	}
}
