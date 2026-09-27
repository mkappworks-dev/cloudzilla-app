package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// json:"-" is the only guard for preferences on any path that writes a model.User as JSON.
func TestUser_JSONOmitsPreferences(t *testing.T) {
	b, err := json.Marshal(model.User{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for key := range got {
		if strings.HasPrefix(key, "email_") || strings.HasPrefix(key, "notify_") || key == "keep_email_private" {
			t.Errorf("model.User JSON exposes preference %q", key)
		}
	}
}
