package seed

import (
	"fmt"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var boardNames = []string{"Roadmap", "Sprint planning", "Bug triage", "Release checklist"}

// seedProjects gives every third eligible repo one or two boards. The first one gets
// two, the second closed, so a seeded instance always has both board states.
func (s *seeder) seedProjects() error {
	picked := 0
	for _, r := range s.repos {
		// A board needs issues and pulls to link; thinner repos would drop card shapes.
		if len(r.issues) < 3 || len(r.pulls) == 0 {
			continue
		}
		// The struct from Create predates the column default, so read the stored flag.
		stored, err := s.svcs.Repo.Get(s.ctx, r.OwnerName, r.Name)
		if err != nil {
			return err
		}
		if !stored.AllowProjects {
			continue
		}
		picked++
		if (picked-1)%3 != 0 {
			continue
		}
		boards := 1 + s.rng.IntN(2)
		if picked == 1 {
			boards = 2
		}
		for i := range boards {
			if err := s.seedBoard(r, boardNames[(picked+i)%len(boardNames)], picked == 1 && i == 1); err != nil {
				return fmt.Errorf("%s/%s: %w", r.OwnerName, r.Name, err)
			}
		}
	}
	return nil
}

func (s *seeder) seedBoard(r *seedRepo, name string, closed bool) error {
	actor := r.owners[0]
	svc := s.svcs.Project
	p, err := svc.CreateProject(s.ctx, r.OwnerName, r.Name, actor.ID, name, "Planning for "+r.Name+".")
	if err != nil {
		return err
	}
	s.report.Projects++
	var cols []int64
	for _, n := range []string{"To do", "In progress", "Done"} {
		c, err := svc.CreateColumn(s.ctx, p.ID, actor.ID, n)
		if err != nil {
			return err
		}
		cols = append(cols, c.ID)
	}

	issues := s.rng.Perm(len(r.issues))
	nextIssue := func() *model.Issue {
		i := r.issues[issues[0]]
		issues = issues[1:]
		return i
	}
	pull := pick(s.rng, r.pulls)

	day := func(d int) *time.Time {
		t := s.opts.Now.AddDate(0, 0, d)
		return &t
	}
	// Org owners are not collaborators, so only a user-owned repo may assign its owner.
	assignable := r.contributors[len(r.owners):]
	if r.OrgID == 0 {
		assignable = r.contributors
	}
	var assignees []int64
	for _, a := range s.sample(assignable, 2) {
		assignees = append(assignees, a.ID)
	}
	var labels []int64
	for _, i := range s.rng.Perm(len(r.labels))[:1+s.rng.IntN(2)] {
		labels = append(labels, r.labels[i].ID)
	}
	referenced := nextIssue()
	linked := nextIssue()
	plain := nextIssue()

	cards := []struct {
		col int
		d   model.CardDetails
	}{
		{0, model.CardDetails{Title: "Draft the release plan", Description: "Outline scope and owners before the next cut.", DueDate: day(3 + s.rng.IntN(25))}},
		{0, model.CardDetails{Title: "Audit open dependencies", Description: "Check for stale pins.", AssigneeIDs: assignees, LabelIDs: labels}},
		{0, model.CardDetails{Title: "Update the contributor guide", Description: "Overdue follow-up from the last review.", DueDate: day(-2 - s.rng.IntN(18))}},
		{0, model.CardDetails{Title: "Triage backlog", Description: fmt.Sprintf("Start with #%d, which several people hit.", referenced.Number)}},
		{1, model.CardDetails{Title: "Track the fix in the tracker", IssueID: &linked.ID}},
		{1, model.CardDetails{PullID: &pull.ID}},
		{2, model.CardDetails{IssueID: &plain.ID}},
	}
	for _, c := range cards {
		if _, err := svc.CreateCard(s.ctx, p.ID, cols[c.col], actor.ID, c.d); err != nil {
			return fmt.Errorf("card %q: %w", c.d.Title, err)
		}
		s.report.Cards++
	}

	// Converting turns the note into a real issue, which the report counts with the rest.
	note, err := svc.CreateCard(s.ctx, p.ID, cols[2], actor.ID, model.CardDetails{
		Title: "Document the upgrade path", Description: "Promoted to an issue once scoped.", AssigneeIDs: assignees, LabelIDs: labels,
	})
	if err != nil {
		return err
	}
	s.report.Cards++
	if _, _, err := svc.ConvertCardToIssue(s.ctx, p.ID, note.ID, actor.ID); err != nil {
		return err
	}
	s.report.Issues++

	if closed {
		_, err = svc.SetProjectClosed(s.ctx, p.ID, actor.ID, true)
	}
	return err
}
