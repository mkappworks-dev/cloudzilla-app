package handler_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const testSecretKey = "0123456789abcdef0123456789abcdef-key"

func withSecretKey(c *config.Config) { c.Security.SecretKey = testSecretKey }

func newSecretEnv(t *testing.T, fork bool) *logAPIEnv {
	t.Helper()
	e := newRunnerEnvWith(t, withSecretKey)
	backend, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.svcs.WithStorage(backend)
	repo, err := e.svcs.Repo.GetByID(context.Background(), e.r.id)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svcs.CISecret.Save(context.Background(), repo, e.r.owner.id, "DEPLOY_TOKEN", "hunter2-secret"); err != nil {
		t.Fatalf("save secret: %v", err)
	}
	if fork {
		testutil.Exec(t, e.db, `UPDATE workflow_runs SET no_secrets = true WHERE repo_id = $1`, e.r.id)
	}
	spec := e.claim(t, e.registerRunner(t, nil))
	return &logAPIEnv{runnerEnv: e, spec: spec, run: spec.RunNumber}
}

func TestCISecret_ReachesANonForkJobAndIsMaskedInItsLog(t *testing.T) {
	e := newSecretEnv(t, false)
	if got := e.spec.Secrets["DEPLOY_TOKEN"]; got != "hunter2-secret" {
		t.Fatalf("claim secrets = %v", e.spec.Secrets)
	}

	// The runner masks first; this proves the server backstop catches an unmasked leak, in every form.
	leak := "plain hunter2-secret b64 " + base64.StdEncoding.EncodeToString([]byte("hunter2-secret")) + "\n"
	wantCode(t, e.postLog(t, e.spec.Token, 0, 0, leak), http.StatusOK)
	rr := e.raw(t, http.MethodGet, fmt.Sprintf("%s/jobs/%d/logs", e.apiBase(), e.spec.JobID), e.r.owner.token, "", nil)
	wantCode(t, rr, http.StatusOK)
	if body := rr.Body.String(); strings.Contains(body, "hunter2-secret") || strings.Contains(body, base64.StdEncoding.EncodeToString([]byte("hunter2-secret"))) {
		t.Errorf("log leaks the secret: %s", body)
	}
}

func TestCISecret_IsWithheldFromAForkRun(t *testing.T) {
	e := newSecretEnv(t, true)
	if len(e.spec.Secrets) != 0 {
		t.Fatalf("a fork run's claim carries secrets: %v", e.spec.Secrets)
	}
}

func TestCISecret_SaveIsRefusedWithoutASecretKey(t *testing.T) {
	e := newCIPushEnv(t)
	repo, err := e.svcs.Repo.GetByID(context.Background(), e.r.id)
	if err != nil {
		t.Fatal(err)
	}
	err = e.svcs.CISecret.Save(context.Background(), repo, e.r.owner.id, "TOKEN", "v")
	if !errors.Is(err, service.ErrCISecretNoKey) {
		t.Fatalf("save without a key: %v", err)
	}
}
