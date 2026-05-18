package service

import (
	"errors"
	"sort"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// ContributorStat holds commit and diff statistics for a single contributor.
type ContributorStat struct {
	Name      string
	Email     string
	Commits   int
	Additions int
	Deletions int
}

// WeeklyActivity holds the total commit count for a calendar week.
type WeeklyActivity struct {
	WeekStart time.Time
	Total     int
}

// CodeFrequencyWeek holds line addition and deletion counts for a calendar week.
type CodeFrequencyWeek struct {
	WeekStart time.Time
	Additions int
	Deletions int
}

// weekStart truncates t to the Monday of its ISO week at midnight UTC.
func weekStart(t time.Time) time.Time {
	t = t.UTC()
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7 so Monday is day 1
	}
	return time.Date(t.Year(), t.Month(), t.Day()-weekday+1, 0, 0, 0, 0, time.UTC)
}

// ref must be the repo's default branch — relying on the bare repo's symbolic
// HEAD is unsafe because it can point at a branch that no longer exists.
func (s *CodeService) GetContributors(owner, repoName, ref string) ([]ContributorStat, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, _, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}
	iter, err := repo.Log(&gogit.LogOptions{From: commit.Hash})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	statsMap := map[string]*ContributorStat{}
	err = iter.ForEach(func(c *object.Commit) error {
		email := c.Author.Email
		name := c.Author.Name
		stat, ok := statsMap[email]
		if !ok {
			stat = &ContributorStat{Name: name, Email: email}
			statsMap[email] = stat
		}
		stat.Commits++
		fileStats, err := c.Stats()
		if err != nil {
			return nil // skip diff errors
		}
		for _, fs := range fileStats {
			stat.Additions += fs.Addition
			stat.Deletions += fs.Deletion
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]ContributorStat, 0, len(statsMap))
	for _, s := range statsMap {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Commits > out[j].Commits
	})
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}

// GetCommitActivity returns last 52 weeks of commit totals, newest last.
func (s *CodeService) GetCommitActivity(owner, repoName string) ([]WeeklyActivity, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	head, err := repo.Head()
	if err != nil {
		return nil, ErrEmptyRepo
	}
	iter, err := repo.Log(&gogit.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	cutoff := time.Now().UTC().Add(-52 * 7 * 24 * time.Hour)
	buckets := map[time.Time]int{}
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Author.When.Before(cutoff) {
			return storer.ErrStop
		}
		ws := weekStart(c.Author.When)
		buckets[ws]++
		return nil
	})
	if err != nil && !errors.Is(err, storer.ErrStop) {
		return nil, err
	}

	now := time.Now().UTC()
	out := make([]WeeklyActivity, 52)
	for i := 51; i >= 0; i-- {
		ws := weekStart(now.Add(-time.Duration(i) * 7 * 24 * time.Hour))
		out[51-i] = WeeklyActivity{WeekStart: ws, Total: buckets[ws]}
	}
	return out, nil
}

// GetCodeFrequency returns last 52 weeks of additions and deletions, newest last.
func (s *CodeService) GetCodeFrequency(owner, repoName string) ([]CodeFrequencyWeek, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	head, err := repo.Head()
	if err != nil {
		return nil, ErrEmptyRepo
	}
	iter, err := repo.Log(&gogit.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	cutoff := time.Now().UTC().Add(-52 * 7 * 24 * time.Hour)
	type weekStat struct{ add, del int }
	buckets := map[time.Time]*weekStat{}
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Author.When.Before(cutoff) {
			return storer.ErrStop
		}
		ws := weekStart(c.Author.When)
		if _, ok := buckets[ws]; !ok {
			buckets[ws] = &weekStat{}
		}
		fileStats, err := c.Stats()
		if err != nil {
			return nil
		}
		for _, fs := range fileStats {
			buckets[ws].add += fs.Addition
			buckets[ws].del += fs.Deletion
		}
		return nil
	})
	if err != nil && !errors.Is(err, storer.ErrStop) {
		return nil, err
	}

	now := time.Now().UTC()
	out := make([]CodeFrequencyWeek, 52)
	for i := 51; i >= 0; i-- {
		ws := weekStart(now.Add(-time.Duration(i) * 7 * 24 * time.Hour))
		var add, del int
		if b, ok := buckets[ws]; ok {
			add, del = b.add, b.del
		}
		out[51-i] = CodeFrequencyWeek{WeekStart: ws, Additions: add, Deletions: del}
	}
	return out, nil
}
