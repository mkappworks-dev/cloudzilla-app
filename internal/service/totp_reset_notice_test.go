package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSendTOTPResetNotice_SendsBeforeReturning(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")

	var got []sentMail
	mail := NewEmailService(config.SMTPConfig{})
	mail.send = func(to, subject, body string) error {
		got = append(got, sentMail{to, subject, body})
		return nil
	}
	admin := NewAdminUserService(store.NewUserStore(db), nil, nil).WithSecurityNotices(mail)

	if err := admin.SendTOTPResetNotice(context.Background(), userID); err != nil {
		t.Fatalf("SendTOTPResetNotice: %v", err)
	}
	wantSubject, _ := adminTOTPResetNotice("")
	if len(got) != 1 || got[0].to != email || got[0].subject != wantSubject {
		t.Fatalf("sent %+v; want one %q notice to %s", got, wantSubject, email)
	}

	mail.send = func(string, string, string) error { return errors.New("smtp down") }
	if err := admin.SendTOTPResetNotice(context.Background(), userID); err == nil || !strings.Contains(err.Error(), "smtp down") {
		t.Errorf("failed send err = %v; want it returned", err)
	}
}
