package fragments

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

func TestLineComments_AuthorAvatarUsesSharedInitialsAndUploadedImage(t *testing.T) {
	const key = "avatars/user/7/" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.png"
	data := view.LineCommentsFragData{
		Key: view.LineCommentKey{Side: "right", Path: "a.go", Line: 3},
		Comments: []view.RenderedLineComment{
			{PullLineComment: model.PullLineComment{AuthorName: "yumi"}, BodyHTML: "<p>nit</p>"},
			{PullLineComment: model.PullLineComment{AuthorName: "daisy"}, BodyHTML: "<p>ok</p>"},
		},
	}
	ctx := components.WithAvatarKeys(context.Background(), map[string]string{"daisy": key})
	var buf bytes.Buffer
	if err := LineComments(data).Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{">Y</span>", ">D</span>", `src="/` + key + `"`} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in %s", want, out)
		}
	}
}
