package handler_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var errLookup = errors.New("lookup down")

type brokenTokens struct{}

func (brokenTokens) ResolveLiveToken(context.Context, *model.Repository, string) (bool, error) {
	return false, errLookup
}

type brokenOutcomeTokens struct{ brokenTokens }

func (brokenOutcomeTokens) ResolveToken(context.Context, *model.Repository, string) (service.TokenOutcome, error) {
	return service.TokenUnknown, errLookup
}

var tokenValue = "czp_" + strings.Repeat("ab", 32)

func TestPushProtection_TokenLookupFails_PassesAndIsLeftToPostReceive(t *testing.T) {
	for name, r := range map[string]service.CloudzillaTokenResolver{"plain": brokenTokens{}, "outcome": brokenOutcomeTokens{}} {
		t.Run(name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			e := newPushEnv(t, db, nil)
			e.svcs.PushScan.WithTokenResolver(r)
			c := testutil.NewPushClient(t, e.r.git)
			tip := c.Commit(e.r.mainTip, tokenFile(tokenValue), "add token")

			refs := e.push(t, c, create(topicRef, tip))

			if got := refs[topicRef]; got != "ok" {
				t.Errorf("status = %q, want ok: a failed read must not refuse the push", got)
			}
			e.waitPostReceive(t, 1)
		})
	}
}

func TestWebEditor_TokenLookupFails_Commits(t *testing.T) {
	db := testutil.OpenTestDB(t)
	e := newPushEnv(t, db, nil)
	e.svcs.PushScan.WithTokenResolver(brokenOutcomeTokens{})

	code, body, _ := e.editA(t, "one\nTOKEN="+tokenValue+"\n")

	if code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303: %.300s", code, body)
	}
	e.waitPostReceive(t, 1)
}
