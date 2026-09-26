package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUser_JSON_OmitsPrivateFields(t *testing.T) {
	b, err := json.Marshal(User{PasswordHash: "hash", KeepEmailPrivate: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"password_hash", "PasswordHash", "keep_email_private", "KeepEmailPrivate"} {
		if strings.Contains(string(b), `"`+key+`"`) {
			t.Errorf("User JSON must not expose %q: %s", key, b)
		}
	}
}
