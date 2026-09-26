package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// Login serializes the full model.User, so preference fields must stay out of its JSON form.
func TestUser_JSONOmitsNotificationPreferences(t *testing.T) {
	b, err := json.Marshal(model.User{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for key := range got {
		if strings.HasPrefix(key, "email_") || strings.HasPrefix(key, "notify_") {
			t.Errorf("model.User JSON exposes preference %q", key)
		}
	}
}
