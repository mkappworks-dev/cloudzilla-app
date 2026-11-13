package service

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCheckWebContent_AlertLookupFails_FailsOpen(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := NewPushScanService(NewSecretAlertService(store.NewSecretAlertStore(db)), config.SecretScanningConfig{PushProtection: true}, "http://x")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	err := svc.CheckWebContent(context.Background(), &model.Repository{ID: 1, OwnerName: "o", Name: "r"}, "main", "a.env",
		[]byte("AWS_ACCESS_KEY_ID="+awsKey+"\n"), 1)

	if err != nil {
		t.Errorf("CheckWebContent = %v, want nil: a failed read must not refuse the commit", err)
	}
}
