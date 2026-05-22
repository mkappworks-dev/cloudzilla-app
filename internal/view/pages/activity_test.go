package pages

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestEventToActivityRow_Push(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"branch":       "main",
		"commit_total": 3,
		"commits": []map[string]any{
			{"sha": "420b44b", "message": "fix: align byline"},
			{"sha": "04992f6", "message": "fix: tighten density"},
		},
	})
	row := eventToActivityRow(model.Event{
		ActorName: "malith",
		OwnerName: "mkappworks",
		RepoName:  "cloudzilla",
		EventType: model.EventPush,
		Payload:   payload,
		CreatedAt: time.Now(),
	})
	if row.Kind != model.EventPush {
		t.Errorf("Kind = %q, want push", row.Kind)
	}
	if row.Branch != "main" {
		t.Errorf("Branch = %q, want main", row.Branch)
	}
	if row.CommitTotal != 3 {
		t.Errorf("CommitTotal = %d, want 3", row.CommitTotal)
	}
	if len(row.Commits) != 2 {
		t.Fatalf("Commits len = %d, want 2", len(row.Commits))
	}
	if row.Commits[0].SHA != "420b44b" || row.Commits[0].Message != "fix: align byline" {
		t.Errorf("Commits[0] = %+v", row.Commits[0])
	}
	if row.RepoName != "mkappworks/cloudzilla" {
		t.Errorf("RepoName = %q", row.RepoName)
	}
}

func TestEventToActivityRow_CommentOnPull(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"number": 327,
		"kind":   "pull",
		"body":   "Worth profiling before merge.",
	})
	row := eventToActivityRow(model.Event{
		ActorName: "daisy",
		OwnerName: "mkappworks",
		RepoName:  "cloudzilla",
		EventType: model.EventComment,
		Payload:   payload,
		CreatedAt: time.Now(),
	})
	if row.Kind != model.EventComment {
		t.Errorf("Kind = %q, want comment", row.Kind)
	}
	if row.Ref != "#327" {
		t.Errorf("Ref = %q, want #327", row.Ref)
	}
	if row.RefURL != "/mkappworks/cloudzilla/pulls/327" {
		t.Errorf("RefURL = %q, want pull URL", row.RefURL)
	}
	if row.Quote != "Worth profiling before merge." {
		t.Errorf("Quote = %q", row.Quote)
	}
}

func TestEventToActivityRow_CommentOnIssue(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"number": 12,
		"kind":   "issue",
		"body":   "Reproduced locally.",
	})
	row := eventToActivityRow(model.Event{
		OwnerName: "acme",
		RepoName:  "widgets",
		EventType: model.EventComment,
		Payload:   payload,
		CreatedAt: time.Now(),
	})
	if row.RefURL != "/acme/widgets/issues/12" {
		t.Errorf("RefURL = %q, want /acme/widgets/issues/12", row.RefURL)
	}
}
