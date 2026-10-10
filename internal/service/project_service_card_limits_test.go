package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func TestProjectService_CardDetailLimits(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	existing := e.note(t, p.ID, col.ID, "existing")

	ids := func(n int) []int64 {
		out := make([]int64, n)
		for i := range out {
			out[i] = int64(i + 1)
		}
		return out
	}
	cases := []struct {
		name string
		d    model.CardDetails
		want error
	}{
		{"description over the cap", model.CardDetails{Title: "t", Description: strings.Repeat("a", service.MaxCardDescriptionBytes+1)}, service.ErrDescriptionTooLong},
		{"description multi-byte over the cap", model.CardDetails{Title: "t", Description: strings.Repeat("é", service.MaxCardDescriptionBytes/2+1)}, service.ErrDescriptionTooLong},
		{"too many assignees", model.CardDetails{Title: "t", AssigneeIDs: ids(service.MaxCardAssignees + 1)}, service.ErrTooManyAssignees},
		{"too many labels", model.CardDetails{Title: "t", LabelIDs: ids(service.MaxCardLabels + 1)}, service.ErrTooManyLabels},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, c.d); !errors.Is(err, c.want) {
				t.Errorf("CreateCard err = %v, want %v", err, c.want)
			}
			if err := e.svc.UpdateCardDetails(ctx, p.ID, existing.ID, e.ownerID, c.d); !errors.Is(err, c.want) {
				t.Errorf("UpdateCardDetails err = %v, want %v", err, c.want)
			}
		})
	}

	atCap := model.CardDetails{Title: "t", Description: strings.Repeat("a", service.MaxCardDescriptionBytes)}
	if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, atCap); err != nil {
		t.Errorf("a description of exactly the cap: %v", err)
	}
}
