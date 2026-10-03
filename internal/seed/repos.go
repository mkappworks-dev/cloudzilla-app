package seed

import (
	"fmt"
	"math"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

type seedRepo struct {
	*model.Repository
	owners       []*person // the user owner, or the org's owners
	contributors []*person // owners first, then writer collaborators
	readers      []*person // who may read a private repo: contributors plus reader collaborators
	weight       float64   // 0..1 popularity; scales history and activity
	langs        []*language
	history      historyResult
	labels       []*model.Label
	milestones   []*model.Milestone
	issues       []*model.Issue
}

func (r *seedRepo) canRead(p *person) bool {
	return !r.Private || containsPerson(r.readers, p)
}

// visitor is anyone who may open issues and comment: everyone on a public repo.
func (s *seeder) visitor(r *seedRepo) *person {
	if r.Private {
		return pick(s.rng, r.readers)
	}
	return s.person()
}

func (s *seeder) repoName(owner string, taken map[string]bool) string {
	for {
		name := pick(s.rng, repoAdjectives) + "-" + pick(s.rng, repoNouns)
		if !taken[owner+"/"+name] {
			taken[owner+"/"+name] = true
			return name
		}
	}
}

func (s *seeder) seedRepos() error {
	taken := map[string]bool{}
	for range s.opts.Repos {
		r := &seedRepo{weight: math.Pow(s.rng.Float64(), 3)}
		r.langs = []*language{pick(s.rng, languages)}
		if chance(s.rng, 0.3) {
			r.langs = append(r.langs, pick(s.rng, languages))
		}
		topic := pick(s.rng, purposeTopics)
		desc := fmt.Sprintf(pick(s.rng, repoPurposes), pick(s.rng, repoNouns), topic)
		private := chance(s.rng, 0.2)

		var pool []*person
		var err error
		if len(s.orgs) > 0 && chance(s.rng, 0.35) {
			o := pick(s.rng, s.orgs)
			name := s.repoName(o.Name, taken)
			r.Repository, err = s.svcs.Org.CreateRepo(s.ctx, o.ID, o.owners[0].ID, name, desc, private, service.RepoInitOptions{})
			r.owners, pool = o.owners, o.members
		} else {
			p := s.person()
			name := s.repoName(p.Username, taken)
			r.Repository, err = s.svcs.Repo.Create(s.ctx, p.ID, p.Username, name, desc, private, service.RepoInitOptions{})
			r.owners, pool = []*person{p}, s.people
		}
		if err != nil {
			return err
		}

		r.contributors = append([]*person{}, r.owners...)
		for _, p := range s.sample(pool, 1+int(r.weight*6)+s.rng.IntN(2), r.owners...) {
			if err := s.svcs.Repo.AddCollaborator(s.ctx, r.Repository, r.owners[0].ID, p.Username, string(model.RoleWriter)); err != nil {
				return fmt.Errorf("collaborator on %s: %w", r.Name, err)
			}
			r.contributors = append(r.contributors, p)
		}
		r.readers = append([]*person{}, r.contributors...)
		if r.Private {
			for _, p := range s.sample(s.people, s.rng.IntN(4), r.contributors...) {
				if err := s.svcs.Repo.AddCollaborator(s.ctx, r.Repository, r.owners[0].ID, p.Username, string(model.RoleReader)); err != nil {
					return fmt.Errorf("reader on %s: %w", r.Name, err)
				}
				r.readers = append(r.readers, p)
			}
		}
		for _, p := range r.contributors {
			if err := s.svcs.Watch.Watch(s.ctx, r.OwnerName, r.Name, p.ID, model.WatchLevelWatching); err != nil {
				return err
			}
		}
		topics := []string{strings.ToLower(r.langs[0].Name), topic}
		if chance(s.rng, 0.5) {
			if extra := pick(s.rng, purposeTopics); extra != topic {
				topics = append(topics, extra)
			}
		}
		if err := s.svcs.Topic.SetTopics(s.ctx, r.ID, topics); err != nil {
			return fmt.Errorf("topics on %s: %w", r.Name, err)
		}

		if err := s.writeHistory(r); err != nil {
			return fmt.Errorf("history of %s/%s: %w", r.OwnerName, r.Name, err)
		}
		s.repos = append(s.repos, r)
		s.report.Repos++
	}
	return nil
}

func (s *seeder) writeHistory(r *seedRepo) error {
	authors := make([]service.GitAuthor, len(r.contributors))
	for i, p := range r.contributors {
		authors[i] = p.author
	}
	end := s.opts.Now.Add(-time.Duration(s.rng.IntN(72)) * time.Hour)
	age := time.Duration(60+s.rng.IntN(300)) * 24 * time.Hour
	spec := historySpec{
		Module:      "example.test/" + r.OwnerName + "/" + r.Name,
		Name:        r.Name,
		Description: r.Description,
		Langs:       r.langs,
		Authors:     authors,
		Start:       end.Add(-age),
		End:         end,
		MainCommits: 4 + int(r.weight*36) + s.rng.IntN(5),
		Tags:        int(r.weight*4) + s.rng.IntN(2),
	}
	for range 1 + int(r.weight*6) + s.rng.IntN(2) {
		spec.Features = append(spec.Features, featureSpec{
			Author:  s.rng.IntN(len(authors)),
			Commits: 1 + s.rng.IntN(3),
			Merge:   chance(s.rng, 0.55),
		})
	}

	dir, err := service.RepoDir(s.root, r.OwnerName, r.Name+".git")
	if err != nil {
		return err
	}
	if r.history, err = buildHistory(dir, s.rng, spec); err != nil {
		return err
	}
	s.report.Commits += spec.MainCommits
	for _, f := range r.history.Features {
		s.report.Commits += f.Commits
		if f.Merge {
			s.report.Commits++
		}
	}
	return s.postReceive(r, dir)
}

// postReceive runs what a real push of every branch would: contributor and
// heatmap stats, primary language, push events, code search and dependencies.
func (s *seeder) postReceive(r *seedRepo, dir string) error {
	gitRepo, err := gogit.PlainOpen(dir)
	if err != nil {
		return err
	}
	cmds := []*packp.Command{{Name: plumbing.NewBranchReferenceName("main"), New: r.history.MainTip}}
	for _, f := range r.history.Features {
		cmds = append(cmds, &packp.Command{Name: plumbing.NewBranchReferenceName(f.Branch), New: f.Tip})
	}
	if err := s.svcs.Repo.OnPostReceive(s.ctx, r.Repository, gitRepo, cmds); err != nil {
		return err
	}
	pusher := r.owners[0]
	for _, ps := range s.svcs.Repo.PushSummaries(gitRepo, cmds) {
		s.svcs.Event.RecordPush(s.ctx, pusher.ID, pusher.Username, &r.ID, r.Name, r.OwnerName, ps)
	}
	if err := s.svcs.Index.IndexRepo(s.ctx, r.Repository); err != nil {
		return err
	}
	return s.svcs.Dependency.ParseAndStore(s.ctx, r.Repository)
}
