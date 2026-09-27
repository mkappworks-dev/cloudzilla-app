package handler_test

// Integration tests: account forms reject passwords bcrypt can't hash with a
// clear message. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const passwordTooLongMessage = "Password is too long"

// bcrypt's limit is 72 bytes, so 72 must still pass the length check.
var passwordLengthCases = []struct {
	name     string
	password string
	tooLong  bool
}{
	{"72 bytes", strings.Repeat("p", 72), false},
	{"73 bytes", strings.Repeat("p", 73), true},
}

func assertPasswordLengthMessage(t *testing.T, body string, tooLong bool) {
	t.Helper()
	if got := strings.Contains(body, passwordTooLongMessage); got != tooLong {
		t.Errorf("password-too-long message shown = %v, want %v; body:\n%s", got, tooLong, body)
	}
}

// The schemaless DB leaves allow_registration at its default (on) without
// touching the shared setting.
func TestPageRegisterSubmit_PasswordLength(t *testing.T) {
	for _, tc := range passwordLengthCases {
		t.Run(tc.name, func(t *testing.T) {
			db := openSchemalessDB(t)

			body := submitForm(t, newFormHandler(t, db), "/register", "", url.Values{
				"username": {"newuser"}, "email": {"newuser@test.invalid"}, "password": {tc.password},
			})

			assertPasswordLengthMessage(t, body, tc.tooLong)
		})
	}
}

func TestPageSetupSubmit_PasswordLength(t *testing.T) {
	for _, tc := range passwordLengthCases {
		t.Run(tc.name, func(t *testing.T) {
			db := openSchemalessDB(t)

			body := submitForm(t, newFormHandler(t, db), "/setup", "", url.Values{
				"username": {"admin"}, "email": {"admin@test.invalid"}, "password": {tc.password},
			})

			assertPasswordLengthMessage(t, body, tc.tooLong)
		})
	}
}

func seedOpenInvitation(t *testing.T, db *sql.DB, email string) string {
	t.Helper()
	inv := &model.Invitation{
		Token:       "testinvite_" + testutil.UniqueSuffix(t),
		Email:       email,
		InvitedByID: testutil.SeedSuperadmin(t, db, testutil.UniqueSuffix(t)),
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.NewInvitationStore(db).Create(context.Background(), inv); err != nil {
		t.Fatalf("seed invitation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM invitations WHERE id = $1`, inv.ID)
	})
	return inv.Token
}

// Only the too-long case runs here: a 72-byte password would create a real
// account in the shared database.
func TestPageInviteSubmit_PasswordTooLong_SaysSo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	token := seedOpenInvitation(t, db, "invitee_"+suffix+"@test.invalid")

	body := submitForm(t, newFormHandler(t, db), "/invite/"+token, "", url.Values{
		"username": {"invitee_" + suffix}, "password": {strings.Repeat("p", 73)},
	})

	assertPasswordLengthMessage(t, body, true)
}
