package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestTimelineEntry_RendersActorAndVerb(t *testing.T) {
	var buf bytes.Buffer
	if err := TimelineEntry(TimelineEntryClosed, "alice", "2 days ago").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, ">alice<") {
		t.Errorf("expected actor name in link text, got: %s", out)
	}
	if !strings.Contains(out, "closed this") {
		t.Errorf("expected verb 'closed this', got: %s", out)
	}
	if !strings.Contains(out, "2 days ago") {
		t.Errorf("expected timestamp text, got: %s", out)
	}
	if !strings.Contains(out, `href="/alice"`) {
		t.Errorf("expected /alice link, got: %s", out)
	}
}

func TestTimelineEntry_VariantPerKind(t *testing.T) {
	cases := []struct {
		kind TimelineEntryKind
		want TimelineVariant
	}{
		{TimelineEntryComment, TimelineDefault},
		{TimelineEntryClosed, TimelineDestructive},
		{TimelineEntryReopened, TimelineSuccess},
		{TimelineEntryMerged, TimelineMerged},
		{TimelineEntryLabeled, TimelineDefault},
		{TimelineEntryAssigned, TimelineDefault},
		{TimelineEntryReviewed, TimelineSuccess},
		{TimelineEntryKind("unknown"), TimelineDefault},
	}
	for _, tc := range cases {
		if got := timelineEntryVariant(tc.kind); got != tc.want {
			t.Errorf("timelineEntryVariant(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestTimelineEntry_VerbPerKind(t *testing.T) {
	cases := []struct {
		kind TimelineEntryKind
		want string
	}{
		{TimelineEntryComment, "commented"},
		{TimelineEntryClosed, "closed this"},
		{TimelineEntryReopened, "reopened this"},
		{TimelineEntryMerged, "merged this"},
		{TimelineEntryLabeled, "added labels"},
		{TimelineEntryAssigned, "assigned"},
		{TimelineEntryReviewed, "reviewed"},
		{TimelineEntryKind("unknown"), ""},
	}
	for _, tc := range cases {
		if got := timelineEntryVerb(tc.kind); got != tc.want {
			t.Errorf("timelineEntryVerb(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
