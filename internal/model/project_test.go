package model

import (
	"slices"
	"testing"
	"time"
)

func TestCardPatch_ApplyReplacesOnlySetFields(t *testing.T) {
	due := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	issue := int64(7)
	cur := CardDetails{Title: "t", Description: "d", DueDate: &due, IssueID: &issue, AssigneeIDs: []int64{1}, LabelIDs: []int64{2}}

	if got := (CardPatch{}).Apply(cur); got.Title != "t" || got.Description != "d" || got.DueDate != &due ||
		got.IssueID != &issue || !slices.Equal(got.AssigneeIDs, []int64{1}) || !slices.Equal(got.LabelIDs, []int64{2}) {
		t.Errorf("empty patch changed the card: %+v", got)
	}

	p := CardPatch{
		Title:       Optional[string]{Set: true, Value: "new"},
		DueDate:     Optional[*time.Time]{Set: true},
		IssueID:     Optional[*int64]{Set: true},
		AssigneeIDs: Optional[[]int64]{Set: true, Value: []int64{}},
	}
	got := p.Apply(cur)
	if got.Title != "new" || got.DueDate != nil || got.IssueID != nil || len(got.AssigneeIDs) != 0 {
		t.Errorf("set fields not replaced: %+v", got)
	}
	if got.Description != "d" || !slices.Equal(got.LabelIDs, []int64{2}) {
		t.Errorf("unset fields changed: %+v", got)
	}
}

func TestCardPatch_Empty(t *testing.T) {
	if !(CardPatch{}).Empty() {
		t.Error("zero patch is not empty")
	}
	if (CardPatch{PullID: Optional[*int64]{Set: true}}).Empty() {
		t.Error("a patch clearing the pull link is empty")
	}
}
