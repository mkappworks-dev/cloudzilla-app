package seed

import (
	"fmt"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var defaultLabels = []struct{ name, color, description string }{
	{"bug", "#d73a4a", "Something isn't working"},
	{"enhancement", "#a2eeef", "New feature or request"},
	{"documentation", "#0075ca", "Improvements or additions to documentation"},
	{"good first issue", "#7057ff", "Good for newcomers"},
	{"help wanted", "#008672", "Extra attention is needed"},
	{"question", "#d876e3", "Further information is requested"},
}

// reactors returns up to n people who can see r.
func (s *seeder) reactors(r *seedRepo, n int) []*person {
	if r.Private {
		return s.sample(r.readers, n)
	}
	return s.sample(s.people, n)
}

func (s *seeder) mention(r *seedRepo, author *person) string {
	if !chance(s.rng, 0.2) {
		return ""
	}
	if p := pick(s.rng, r.contributors); p.ID != author.ID {
		return p.Username
	}
	return ""
}

// discuss adds up to max comments by visitors of r, each with a few reactions.
func (s *seeder) discuss(r *seedRepo, max int, create func(p *person, body string) (*model.Comment, error)) error {
	for range s.rng.IntN(max + 1) {
		p := s.visitor(r)
		c, err := create(p, commentBody(s.rng, s.mention(r, p)))
		if err != nil {
			return err
		}
		s.report.Comments++
		err = s.react(s.reactors(r, s.rng.IntN(3)), func(uid int64, emoji string) (bool, error) {
			return s.svcs.Reaction.Toggle(s.ctx, uid, c.ID, emoji)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *seeder) seedIssues() error {
	for _, r := range s.repos {
		for _, l := range defaultLabels {
			label, err := s.svcs.Label.Create(s.ctx, r.OwnerName, r.Name, l.name, l.color, l.description)
			if err != nil {
				return err
			}
			r.labels = append(r.labels, label)
		}
		for i := range int(r.weight*3) + s.rng.IntN(2) {
			due := s.opts.Now.AddDate(0, 0, s.rng.IntN(120)-30)
			m, err := s.svcs.Milestone.Create(s.ctx, r.OwnerName, r.Name,
				fmt.Sprintf("v0.%d", len(r.history.Tags)+i+1), "Planned work for the next release.", &due)
			if err != nil {
				return err
			}
			r.milestones = append(r.milestones, m)
		}
		if len(r.milestones) > 1 && chance(s.rng, 0.5) {
			if _, err := s.svcs.Milestone.Close(s.ctx, r.OwnerName, r.Name, r.milestones[0].Number, nil); err != nil {
				return err
			}
		}

		for range int(r.weight*25) + s.rng.IntN(4) {
			if err := s.seedIssue(r); err != nil {
				return fmt.Errorf("%s/%s: %w", r.OwnerName, r.Name, err)
			}
		}
	}
	return nil
}

func (s *seeder) seedIssue(r *seedRepo) error {
	author := s.visitor(r)
	visibility := "public"
	if containsPerson(r.contributors, author) && chance(s.rng, 0.05) {
		visibility = "private"
	}
	issue, err := s.svcs.Issue.Create(s.ctx, r.OwnerName, r.Name, author.ID,
		fill(s.rng, pick(s.rng, issueTemplates)), issueBody(s.rng, r.langs[0]), visibility)
	if err != nil {
		return err
	}
	r.issues = append(r.issues, issue)
	s.report.Issues++
	s.event(author, r, model.EventIssueOpened, map[string]any{"number": issue.Number, "title": issue.Title})

	for _, i := range s.rng.Perm(len(r.labels))[:s.rng.IntN(3)] {
		if err := s.svcs.Label.AddToIssue(s.ctx, r.OwnerName, r.Name, issue.Number, r.labels[i].ID); err != nil {
			return err
		}
	}
	if chance(s.rng, 0.4) {
		if err := s.svcs.Assignee.AddToIssue(s.ctx, r.OwnerName, r.Name, issue.Number, pick(s.rng, r.contributors).Username); err != nil {
			return err
		}
	}
	if len(r.milestones) > 0 && chance(s.rng, 0.3) {
		if err := s.svcs.Milestone.SetIssue(s.ctx, issue.ID, &pick(s.rng, r.milestones).ID); err != nil {
			return err
		}
	}

	err = s.discuss(r, 1+int(r.weight*8), func(p *person, body string) (*model.Comment, error) {
		c, err := s.svcs.Comment.CreateForIssue(s.ctx, *r.Repository, issue.ID, issue.Number, p.ID, p.Username, body)
		if err != nil {
			return nil, err
		}
		s.svcs.Notification.NotifyIssueComment(s.ctx, *r.Repository, *issue, p.ID, p.Username)
		s.event(p, r, model.EventComment, map[string]any{"number": issue.Number, "kind": "issue", "body": body})
		return c, nil
	})
	if err != nil {
		return err
	}

	if chance(s.rng, 0.35) {
		closer := pick(s.rng, r.contributors)
		closed, err := s.svcs.Issue.SetState(s.ctx, r.OwnerName, r.Name, issue.Number, model.IssueStateClosed, closer.ID, closer.Username)
		if err != nil {
			return err
		}
		s.svcs.Notification.NotifyIssueStateChange(s.ctx, *r.Repository, *closed, closer.ID, closer.Username)
		s.event(closer, r, model.EventIssueClosed, map[string]any{"number": issue.Number})
	}
	return nil
}

func pullBody(f featureResult, closes *model.Issue) string {
	var b strings.Builder
	b.WriteString("## Summary\n\n" + f.Title + ".\n\n## Changes\n\n")
	seen := map[string]bool{}
	for _, h := range f.Hunks {
		if !seen[h.Path] {
			seen[h.Path] = true
			b.WriteString("- `" + h.Path + "`\n")
		}
	}
	if closes != nil {
		fmt.Fprintf(&b, "\nCloses #%d\n", closes.Number)
	}
	return b.String()
}

func (s *seeder) reviewState(f featureResult, closed bool) string {
	switch {
	case f.Merge:
		return pick(s.rng, []string{"approved", "approved", "approved", "commented"})
	case closed:
		return pick(s.rng, []string{"changes_requested", "commented"})
	}
	return pick(s.rng, []string{"approved", "changes_requested", "commented"})
}

func (s *seeder) seedPulls() error {
	for _, r := range s.repos {
		for _, f := range r.history.Features {
			if err := s.seedPull(r, f); err != nil {
				return fmt.Errorf("%s/%s %s: %w", r.OwnerName, r.Name, f.Branch, err)
			}
		}
	}
	return nil
}

func (s *seeder) seedPull(r *seedRepo, f featureResult) error {
	author := r.contributors[f.Author]
	outcome := model.PRStateOpen
	draft := false
	switch roll := s.rng.Float64(); {
	case f.Merge:
		outcome = model.PRStateMerged
	case roll < 0.3:
		outcome = model.PRStateClosed
	case roll < 0.5:
		draft = true
	}
	var closes *model.Issue
	if len(r.issues) > 0 && chance(s.rng, 0.3) {
		closes = pick(s.rng, r.issues)
	}

	pr, err := s.svcs.Pull.Create(s.ctx, r.OwnerName, r.Name, author.ID, f.Title, pullBody(f, closes), f.Branch, "main", draft, nil)
	if err != nil {
		return err
	}
	s.report.Pulls++
	s.event(author, r, model.EventPROpened, map[string]any{"number": pr.Number, "title": pr.Title})
	if err := s.svcs.Assignee.AddToPull(s.ctx, r.OwnerName, r.Name, pr.Number, author.Username); err != nil {
		return err
	}
	if chance(s.rng, 0.6) {
		if err := s.svcs.Label.AddToPull(s.ctx, r.OwnerName, r.Name, pr.Number, pick(s.rng, r.labels[:2]).ID); err != nil {
			return err
		}
	}

	reviewers := s.sample(r.contributors, 1+s.rng.IntN(3), author)
	for _, rv := range reviewers {
		state := s.reviewState(f, outcome == model.PRStateClosed)
		if _, err := s.svcs.PullReview.SubmitReview(s.ctx, r.OwnerName, r.Name, pr.Number, rv.ID, rv.Username, state, pick(s.rng, reviewBodies[state])); err != nil {
			return err
		}
		s.svcs.Notification.NotifyPRReview(s.ctx, *r.Repository, *pr, rv.ID, rv.Username)
	}
	// After a merge the PR diff is empty, so only unmerged PRs get line comments.
	if outcome != model.PRStateMerged && len(reviewers) > 0 {
		for range s.rng.IntN(4) {
			h := pick(s.rng, f.Hunks)
			i := s.rng.IntN(len(h.Lines))
			body := pick(s.rng, lineCommentBodies)
			if line := h.Lines[i]; strings.Contains(line, "item") && chance(s.rng, 0.5) {
				body = "Clearer name?\n```suggestion\n" + strings.ReplaceAll(line, "item", "entry") + "\n```"
			}
			rv := pick(s.rng, reviewers)
			if _, err := s.svcs.PullLineComment.Create(s.ctx, r.OwnerName, r.Name, pr.Number, rv.ID, rv.Username, h.Path, "right", h.Start+i, body); err != nil {
				return err
			}
			s.report.Comments++
		}
	}

	err = s.discuss(r, 3, func(p *person, body string) (*model.Comment, error) {
		c, err := s.svcs.Comment.CreateForPull(s.ctx, *r.Repository, pr.ID, pr.Number, p.ID, p.Username, body)
		if err != nil {
			return nil, err
		}
		s.svcs.Notification.NotifyPRComment(s.ctx, *r.Repository, *pr, p.ID, p.Username)
		s.event(p, r, model.EventComment, map[string]any{"number": pr.Number, "kind": "pull", "body": body})
		return c, nil
	})
	if err != nil {
		return err
	}

	if outcome == model.PRStateOpen {
		return nil
	}
	actor := r.owners[0]
	done, err := s.svcs.Pull.SetState(s.ctx, r.OwnerName, r.Name, pr.Number, outcome)
	if err != nil {
		return err
	}
	pullEvent, feedEvent := model.PullEventClosed, model.EventPRClosed
	if outcome == model.PRStateMerged {
		pullEvent, feedEvent = model.PullEventMerged, model.EventPRMerged
	}
	if err := s.svcs.PullEvent.Record(s.ctx, pr.ID, actor.ID, actor.Username, pullEvent, ""); err != nil {
		return err
	}
	s.svcs.Notification.NotifyPRStateChange(s.ctx, *r.Repository, *done, actor.ID, actor.Username)
	s.event(actor, r, feedEvent, map[string]any{"number": pr.Number})
	return nil
}

func (s *seeder) seedDiscussions() error {
	categories, err := s.svcs.Discussion.ListCategories(s.ctx)
	if err != nil || len(categories) == 0 {
		return fmt.Errorf("no discussion categories (err %v)", err)
	}
	for _, r := range s.repos {
		if !chance(s.rng, 0.25+r.weight*0.5) {
			continue
		}
		for range 1 + int(r.weight*6) {
			cat := pick(s.rng, categories)
			titles, ok := discussionTitles[cat.Name]
			if !ok {
				titles = discussionTitles["General"]
			}
			author := s.visitor(r)
			if cat.Name == "Announcements" {
				author = r.owners[0]
			}
			title := strings.ReplaceAll(fill(s.rng, pick(s.rng, titles)), "{repo}", r.Name)
			d, err := s.svcs.Discussion.Create(s.ctx, r.OwnerName, r.Name, author.ID, author.Username, cat.ID, title,
				"I've been thinking about this for a while. "+pick(s.rng, commentBodies))
			if err != nil {
				return err
			}
			s.report.Discussions++

			var replies []*model.DiscussionReply
			for range s.rng.IntN(6) {
				p := s.visitor(r)
				var parent *int64
				if len(replies) > 0 && chance(s.rng, 0.3) {
					parent = &pick(s.rng, replies).ID
				}
				reply, err := s.svcs.Discussion.CreateReply(s.ctx, d.ID, p.ID, p.Username, commentBody(s.rng, s.mention(r, p)), parent)
				if err != nil {
					return err
				}
				replies = append(replies, reply)
				s.svcs.Notification.NotifyDiscussionReply(s.ctx, *r.Repository, *d, p.ID, p.Username)
				err = s.react(s.reactors(r, s.rng.IntN(3)), func(uid int64, emoji string) (bool, error) {
					return s.svcs.Reaction.ToggleReply(s.ctx, uid, reply.ID, emoji)
				})
				if err != nil {
					return err
				}
			}
			if cat.Name == "Q&A" && len(replies) > 0 && chance(s.rng, 0.6) {
				if err := s.svcs.Discussion.SetAnswer(s.ctx, d.ID, &replies[0].ID); err != nil {
					return err
				}
			}
			err = s.react(s.reactors(r, s.rng.IntN(5)), func(uid int64, emoji string) (bool, error) {
				return s.svcs.Reaction.ToggleDiscussion(s.ctx, uid, d.ID, emoji)
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *seeder) seedReleases() error {
	for _, r := range s.repos {
		for i, t := range r.history.Tags {
			notes := "## What's changed\n\n- " + strings.Join(t.Notes, "\n- ") + "\n"
			pre := i == len(r.history.Tags)-1 && strings.HasPrefix(t.Name, "v0.") && chance(s.rng, 0.3)
			author := r.owners[0]
			rel, err := s.svcs.Release.Create(s.ctx, r.OwnerName, r.Name, t.Name, "", t.Name, notes, pre, false, author.ID)
			if err != nil {
				return fmt.Errorf("%s/%s %s: %w", r.OwnerName, r.Name, t.Name, err)
			}
			s.report.Releases++
			s.event(author, r, model.EventReleasePublished, map[string]any{"tag": rel.TagName, "name": rel.Name})
		}
	}
	return nil
}

// seedSocial stars and watches repos in proportion to their popularity.
func (s *seeder) seedSocial() error {
	if len(s.repos) == 0 {
		return nil
	}
	starred := map[[2]int64]bool{}
	for _, p := range s.people {
		for range s.rng.IntN(30) {
			r := pick(s.rng, s.repos)
			if s.rng.Float64() > r.weight+0.15 || !r.canRead(p) || starred[[2]int64{p.ID, r.ID}] {
				continue
			}
			starred[[2]int64{p.ID, r.ID}] = true
			if err := s.svcs.Star.Star(s.ctx, r.OwnerName, r.Name, p.ID); err != nil {
				return err
			}
			s.report.Stars++
			s.event(p, r, model.EventStar, map[string]any{})
			if chance(s.rng, 0.3) {
				level := pick(s.rng, []string{model.WatchLevelWatching, model.WatchLevelReleasesOnly})
				if err := s.svcs.Watch.Watch(s.ctx, r.OwnerName, r.Name, p.ID, level); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *seeder) seedGists() error {
	for range max(s.opts.Users/2, 1) {
		p := s.person()
		lang := pick(s.rng, languages)
		var files []model.GistFile
		names := map[string]bool{}
		for range 1 + s.rng.IntN(3) {
			id := ident(s.rng)
			name := id.snake() + lang.Ext
			if names[name] {
				continue
			}
			names[name] = true
			files = append(files, model.GistFile{Filename: name, Content: lang.header + lang.block(s.rng, id)})
		}
		desc := fill(s.rng, pick(s.rng, gistDescriptions))
		if _, err := s.svcs.Gist.Create(s.ctx, p.ID, p.Username, desc, chance(s.rng, 0.8), files); err != nil {
			return err
		}
		s.report.Gists++
	}
	return nil
}
