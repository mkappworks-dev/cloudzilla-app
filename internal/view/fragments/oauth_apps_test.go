package fragments

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

func TestOAuthAppsList_RevealsClientSecretOnlyWhenGiven(t *testing.T) {
	const hash = "$2a$10$storedbcrypthash"
	apps := []model.OAuthApp{{ID: 3, Name: "CI bot", ClientID: "c0ffee", ClientSecret: hash}}
	render := func(data view.OAuthAppsFragData) string {
		t.Helper()
		var sb strings.Builder
		if err := OAuthAppsList(data).Render(context.Background(), &sb); err != nil {
			t.Fatalf("render: %v", err)
		}
		return sb.String()
	}

	created := render(view.OAuthAppsFragData{Apps: apps, NewClientID: "c0ffee", NewClientSecret: "raw-secret-once"})
	if !strings.Contains(created, "raw-secret-once") {
		t.Error("create response does not show the new client secret")
	}
	if strings.Contains(created, hash) {
		t.Error("create response renders the stored secret hash")
	}

	rerendered := render(view.OAuthAppsFragData{Apps: apps})
	if strings.Contains(rerendered, "Copy the client secret now") {
		t.Error("re-render shows a client-secret reveal")
	}
	if strings.Contains(rerendered, hash) {
		t.Error("re-render renders the stored secret hash")
	}
}
