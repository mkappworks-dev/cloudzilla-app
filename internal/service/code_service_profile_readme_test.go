package service

import (
	"html/template"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

func TestGetProfileReadme_MissingRepo(t *testing.T) {
	svc := NewCodeService(config.GitConfig{ReposRoot: "/tmp/nonexistent_cloudzilla_test"})
	got := svc.GetProfileReadme("alice", "alice", "main")
	if got != template.HTML("") {
		t.Fatalf("expected empty HTML for missing repo, got: %q", got)
	}
}
