package handler_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type hookCatcher struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits []caughtHook
}

type caughtHook struct {
	event, signature string
	body             []byte
}

func newHookCatcher(t *testing.T) *hookCatcher {
	t.Helper()
	c := &hookCatcher{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.hits = append(c.hits, caughtHook{r.Header.Get("X-Cloudzilla-Event"), r.Header.Get("X-Hub-Signature-256"), body})
		c.mu.Unlock()
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *hookCatcher) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.hits)
}

func (c *hookCatcher) first() caughtHook {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[0]
}

// finishRun claims and finishes every Job of the env's first Run with result.
func (e *runnerEnv) finishRun(t *testing.T, result model.CIStatus) {
	t.Helper()
	ctx := context.Background()
	for {
		job, err := e.svcs.WorkflowRun.ClaimJob(ctx, []string{e.label}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			return
		}
		if err := e.svcs.WorkflowRun.FinishJob(ctx, job.ID, result, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkflowRunWebhook_FiresOnceOnCompletionToSubscribedHooksOnly(t *testing.T) {
	e := newRunnerEnvWith(t, func(c *config.Config) { c.Webhook.AllowLocalNetworks = true })
	ctx := context.Background()
	subscribed, other := newHookCatcher(t), newHookCatcher(t)
	if _, err := e.svcs.Webhook.Create(ctx, e.r.id, subscribed.srv.URL, "s3cret", "push,workflow_run"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svcs.Webhook.Create(ctx, e.r.id, other.srv.URL, "s3cret", ""); err != nil {
		t.Fatal(err)
	}

	e.finishRun(t, model.CIStatusFailure)

	eventually(t, "the workflow_run delivery", func() bool { return subscribed.count() > 0 })
	time.Sleep(300 * time.Millisecond)
	if subscribed.count() != 1 {
		t.Errorf("deliveries = %d, want exactly 1 for one completion", subscribed.count())
	}
	if other.count() != 0 {
		t.Errorf("a hook that did not subscribe got %d deliveries", other.count())
	}
	hit := subscribed.first()
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(hit.body)
	if hit.event != "workflow_run" || hit.signature != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Errorf("event %q signature %q", hit.event, hit.signature)
	}
	var p struct {
		Action      string `json:"action"`
		WorkflowRun struct {
			Number     int    `json:"number"`
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
			HeadSHA    string `json:"head_sha"`
			HeadBranch string `json:"head_branch"`
			HTMLURL    string `json:"html_url"`
		} `json:"workflow_run"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(hit.body, &p); err != nil {
		t.Fatal(err)
	}
	wr := p.WorkflowRun
	if p.Action != "completed" || wr.Number != 1 || wr.Name != "CI" || wr.Conclusion != "failure" || wr.HeadSHA != e.sha ||
		wr.HeadBranch != "main" || wr.HTMLURL != "http://localhost:8080"+e.r.path+"/actions/runs/1" ||
		p.Repository.ID != e.r.id || p.Repository.FullName != e.r.path[1:] {
		t.Errorf("payload = %s", hit.body)
	}
}

func TestWorkflowRunWebhook_CancelFiresToo(t *testing.T) {
	e := newRunnerEnvWith(t, func(c *config.Config) { c.Webhook.AllowLocalNetworks = true })
	c := newHookCatcher(t)
	if _, err := e.svcs.Webhook.Create(context.Background(), e.r.id, c.srv.URL, "", "workflow_run"); err != nil {
		t.Fatal(err)
	}
	repo, _ := e.svcs.Repo.GetByID(context.Background(), e.r.id)

	if err := e.svcs.WorkflowRun.Cancel(context.Background(), repo, 1, e.r.owner.id); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the delivery of a cancelled run", func() bool { return c.count() == 1 })
}

func TestWorkflowRunWebhook_EventCanBeSelectedOnAnExistingHook(t *testing.T) {
	e := newRunnerEnvWith(t, func(c *config.Config) { c.Webhook.AllowLocalNetworks = true })
	ctx := context.Background()
	wh, err := e.svcs.Webhook.Create(ctx, e.r.id, "http://127.0.0.1:9/hook", "", "")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/repos%s/hooks/%d", e.r.path, wh.ID)

	wantCode(t, e.call(t, http.MethodPatch, path, e.r.owner.token, map[string]string{"events": "push,workflow_run"}), http.StatusNoContent)

	hooks, _ := e.svcs.Webhook.ListByRepo(ctx, e.r.id)
	if len(hooks) != 1 || hooks[0].Events != "push,workflow_run" {
		t.Errorf("hooks = %+v", hooks)
	}
}
