package router_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestIssuePage_SharesOneHighlightBudgetAcrossComments(t *testing.T) {
	h, svc, db := newTestRouter(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	repoName := "mdhl_" + suffix
	repo, err := svc.Repo.Create(ctx, ownerID, owner, repoName, "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("Repo.Create: %v", err)
	}

	// 200 KiB each: under the 256 KiB document cap, and five of them fill the 1 MiB request budget.
	fence := "```go\n" + strings.Repeat("// "+strings.Repeat("x", 97)+"\n", 2048) + "```\n"
	issue, err := svc.Issue.Create(ctx, owner, repoName, ownerID, "big fences", fence, "")
	if err != nil {
		t.Fatalf("Issue.Create: %v", err)
	}
	for range 6 {
		if _, err := svc.Comment.CreateForIssue(ctx, *repo, issue.ID, issue.Number, ownerID, owner, fence); err != nil {
			t.Fatalf("CreateForIssue: %v", err)
		}
	}

	path := "/" + owner + "/" + repoName + "/issues/" + strconv.Itoa(issue.Number)
	rr := serve(h, browserRequest(http.MethodGet, path, makeJWT(t, ownerID, owner), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, rr.Code)
	}
	body := rr.Body.String()
	highlighted := strings.Count(body, `<pre class="hl"><code class="language-go">`)
	plain := strings.Count(body, `<pre><code class="language-go">`)
	if highlighted+plain != 7 {
		t.Fatalf("%d highlighted + %d plain fences, want 7 in all", highlighted, plain)
	}
	if highlighted == 0 || highlighted > 5 {
		t.Errorf("%d fences highlighted, want 1 to 5: the request budget fits five", highlighted)
	}
	if first := strings.Index(body, `<code class="language-go">`); first < 0 || !strings.HasSuffix(body[:first], `<pre class="hl">`) {
		t.Error("the issue body is not highlighted: it should be first in line for the budget")
	}
}
