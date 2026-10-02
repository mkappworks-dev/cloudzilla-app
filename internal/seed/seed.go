// Package seed fills a fresh Cloudzilla instance with realistic test data
// through the same services the handlers use.
package seed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const (
	AdminUsername   = "siteadmin"
	AdminEmail      = "admin@example.test"
	DefaultPassword = "cloudzilla-seed"
)

// ErrNotFresh is returned when the target already holds accounts or repositories:
// the seed never mixes its rows with real data.
var ErrNotFresh = errors.New("seed only runs against a fresh instance")

type Options struct {
	Users    int
	Orgs     int
	Repos    int
	Seed     uint64
	Password string
	Now      time.Time
	Log      io.Writer
}

type Report struct {
	Users, Orgs, Repos, Commits, Issues, Pulls, Comments, Discussions, Releases, Stars, Gists int
}

type seeder struct {
	ctx    context.Context
	svcs   *service.Services
	root   string
	opts   Options
	rng    *rand.Rand
	report Report

	people []*person
	orgs   []*org
	repos  []*seedRepo
}

// Run seeds svcs' database and reposRoot. Both must be empty.
func Run(ctx context.Context, svcs *service.Services, reposRoot string, opts Options) (Report, error) {
	if svcs.SiteSetting.IsSetupComplete(ctx) {
		return Report{}, fmt.Errorf("%w: the database already has accounts", ErrNotFresh)
	}
	if entries, err := os.ReadDir(reposRoot); err == nil && len(entries) > 0 {
		return Report{}, fmt.Errorf("%w: %s is not empty", ErrNotFresh, reposRoot)
	}
	if opts.Password == "" {
		opts.Password = DefaultPassword
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	s := &seeder{
		ctx: ctx, svcs: svcs, root: reposRoot, opts: opts,
		rng: rand.New(rand.NewPCG(opts.Seed, opts.Seed+1)),
	}
	steps := []struct {
		name string
		run  func() error
	}{
		{"people", s.seedPeople},
		{"organizations", s.seedOrgs},
		{"repositories", s.seedRepos},
		{"issues", s.seedIssues},
		{"pull requests", s.seedPulls},
		{"discussions", s.seedDiscussions},
		{"releases", s.seedReleases},
		{"stars and watches", s.seedSocial},
		{"gists", s.seedGists},
	}
	for _, step := range steps {
		start := time.Now()
		if err := step.run(); err != nil {
			return s.report, fmt.Errorf("seed %s: %w", step.name, err)
		}
		_, _ = fmt.Fprintf(opts.Log, "%-18s done in %s\n", step.name, time.Since(start).Round(time.Millisecond))
	}
	return s.report, nil
}

// skewed returns an index in [0, n) biased toward 0, so a few people and repos
// carry most of the activity, as on a real forge.
func (s *seeder) skewed(n int) int {
	return int(math.Pow(s.rng.Float64(), 2) * float64(n))
}

func (s *seeder) person() *person { return s.people[s.skewed(len(s.people))] }

// sample returns up to n distinct people from pool, excluding skip.
func (s *seeder) sample(pool []*person, n int, skip ...*person) []*person {
	out := make([]*person, 0, n)
	for _, i := range s.rng.Perm(len(pool)) {
		if len(out) == n {
			break
		}
		p := pool[i]
		if !containsPerson(skip, p) {
			out = append(out, p)
		}
	}
	return out
}

func containsPerson(xs []*person, p *person) bool {
	for _, x := range xs {
		if x.ID == p.ID {
			return true
		}
	}
	return false
}

func (s *seeder) event(actor *person, r *seedRepo, eventType string, payload map[string]any) {
	s.svcs.Event.Record(s.ctx, actor.ID, actor.Username, &r.ID, r.Name, r.OwnerName, eventType, payload)
}

func (s *seeder) react(people []*person, toggle func(userID int64, emoji string) (bool, error)) error {
	emojis := []string{"+1", "heart", "rocket", "eyes", "hooray", "laugh"}
	for _, p := range people {
		if _, err := toggle(p.ID, pick(s.rng, emojis)); err != nil {
			return err
		}
	}
	return nil
}
