package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestKanbanColumn_RendersCards(t *testing.T) {
	col := KanbanColumnData{
		ID: 7, Title: "In Progress",
		Cards: []KanbanCardData{
			{ID: 1, Title: "card a", IssueNumber: 12, RepoFullName: "o/r"},
			{ID: 2, Title: "card b", IssueNumber: 13, RepoFullName: "o/r"},
		},
	}
	var buf bytes.Buffer
	KanbanColumn(col).Render(context.Background(), &buf)
	out := buf.String()
	if !strings.Contains(out, "In Progress") || !strings.Contains(out, "card a") {
		t.Errorf("missing fields: %s", out)
	}
	if !strings.Contains(out, `data-column-id="7"`) {
		t.Errorf("expected data-column-id for drop target")
	}
}
