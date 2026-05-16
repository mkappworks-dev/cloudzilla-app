package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestReviewerPicker_RendersSuggestedAndAll(t *testing.T) {
	d := ReviewerPickerData{
		Suggested: []ReviewerOption{
			{Username: "alice", Reason: "CODEOWNERS", Selected: true},
		},
		All: []ReviewerOption{
			{Username: "alice", Reason: "CODEOWNERS", Selected: true},
			{Username: "bob", Reason: "recent contributor", Selected: false},
		},
	}
	var buf bytes.Buffer
	if err := ReviewerPicker(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Suggested") {
		t.Errorf("expected 'Suggested' heading, got: %s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected username 'alice', got: %s", out)
	}
	if !strings.Contains(out, "CODEOWNERS") {
		t.Errorf("expected reason 'CODEOWNERS', got: %s", out)
	}
	if !strings.Contains(out, "All users") {
		t.Errorf("expected 'All users' summary, got: %s", out)
	}
	if !strings.Contains(out, "bob") {
		t.Errorf("expected username 'bob' in All list, got: %s", out)
	}
}

func TestReviewerPicker_CheckedState(t *testing.T) {
	d := ReviewerPickerData{
		All: []ReviewerOption{
			{Username: "alice", Selected: true},
			{Username: "bob", Selected: false},
		},
	}
	var buf bytes.Buffer
	if err := ReviewerPicker(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "checked") {
		t.Errorf("expected 'checked' attribute for selected reviewer, got: %s", out)
	}
	if strings.Count(out, "checked") != 1 {
		t.Errorf("expected exactly 1 'checked' attribute, got %d in: %s", strings.Count(out, "checked"), out)
	}
}

func TestReviewerPicker_OmitsSuggestedSectionWhenEmpty(t *testing.T) {
	d := ReviewerPickerData{
		Suggested: nil,
		All:       []ReviewerOption{{Username: "carol"}},
	}
	var buf bytes.Buffer
	if err := ReviewerPicker(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	if strings.Contains(out, "Suggested") {
		t.Errorf("expected no 'Suggested' section when Suggested is empty, got: %s", out)
	}
	if !strings.Contains(out, "carol") {
		t.Errorf("expected 'carol' in All list, got: %s", out)
	}
}
