package fragments

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

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

func suggestionData(c model.PullLineComment, canWrite, open bool) view.LineCommentsFragData {
	c.IsSuggestion, c.SuggestionBody, c.DiffSide, c.Line, c.ID, c.AuthorName = true, "if ok {", "right", 80, 5, "yumi"
	return view.LineCommentsFragData{
		Owner: "o", RepoName: "r", PullNumber: 3, CanWrite: canWrite, PullOpen: open,
		Key:      view.LineCommentKey{Side: "right", Path: "a.go", Line: 80},
		Comments: []view.RenderedLineComment{{PullLineComment: c, BodyHTML: "<p>use the helper</p>", Original: "if !ok {", HasOriginal: true}},
	}
}

func renderLineComments(t *testing.T, d view.LineCommentsFragData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := LineComments(d).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestLineComments_PendingSuggestionShowsDiffAndApply(t *testing.T) {
	out := renderLineComments(t, suggestionData(model.PullLineComment{}, true, true))
	for _, want := range []string{"Suggested change", "-if !ok {", "+if ok {", "Apply suggestion", "/pulls/3/line_comments/5/apply"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

func TestLineComments_SuggestionHasNoApplyWithoutWriteAccessOrOnClosedPull(t *testing.T) {
	for name, d := range map[string]view.LineCommentsFragData{
		"read-only": suggestionData(model.PullLineComment{}, false, true),
		"closed":    suggestionData(model.PullLineComment{}, true, false),
	} {
		if out := renderLineComments(t, d); strings.Contains(out, "Apply suggestion") || !strings.Contains(out, "+if ok {") {
			t.Errorf("%s: want the suggestion without an Apply button: %s", name, out)
		}
	}
}

func TestLineComments_AppliedSuggestionNamesTheCommit(t *testing.T) {
	at := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	out := renderLineComments(t, suggestionData(model.PullLineComment{AppliedAt: &at, AppliedSHA: "4e1c0d2aaaaaaaa", AppliedBy: "daisy"}, true, true))
	for _, want := range []string{">Applied<", "daisy", "applied this in", `href="/o/r/commit/4e1c0d2aaaaaaaa"`, ">4e1c0d2<"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "Apply suggestion") {
		t.Error("an applied suggestion must not offer Apply")
	}
}
