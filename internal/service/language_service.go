package service

import (
	"context"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// README/Markdown/configs/lockfiles are intentionally excluded — composition is about *code*.
var extToLang = map[string]string{
	".go":    "Go",
	".js":    "JavaScript",
	".jsx":   "JavaScript",
	".ts":    "TypeScript",
	".tsx":   "TypeScript",
	".py":    "Python",
	".rb":    "Ruby",
	".rs":    "Rust",
	".java":  "Java",
	".kt":    "Kotlin",
	".swift": "Swift",
	".c":     "C",
	".h":     "C",
	".cpp":   "C++",
	".hpp":   "C++",
	".cs":    "C#",
	".php":   "PHP",
	".sh":    "Shell",
	".html":  "HTML",
	".css":   "CSS",
	".scss":  "Sass",
	".templ": "Templ",
	".sql":   "SQL",
}

var excludedDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"target":       true,
}

type LanguageService struct {
	code  *CodeService
	repos *RepoService
	orgs  *OrgService
	cache sync.Map // key="owner/repo:ref" → cacheEntry
}

// Negative entries (err != nil) use the shorter TTL so a permanent failure can't hammer the tree walk on every refresh.
type cacheEntry struct {
	comp     map[string]int64
	err      error
	cachedAt time.Time
}

const (
	langCacheTTL         = 10 * time.Minute
	langCacheNegativeTTL = 30 * time.Second
)

func NewLanguageService(code *CodeService, repos *RepoService, orgs *OrgService) *LanguageService {
	return &LanguageService{code: code, repos: repos, orgs: orgs}
}

func (s *LanguageService) Composition(ctx context.Context, owner, repoName, ref string) (map[string]int64, error) {
	key := owner + "/" + repoName + ":" + ref
	if v, ok := s.cache.Load(key); ok {
		e := v.(cacheEntry)
		ttl := langCacheTTL
		if e.err != nil {
			ttl = langCacheNegativeTTL
		}
		if time.Since(e.cachedAt) < ttl {
			return e.comp, e.err
		}
	}
	comp := make(map[string]int64)
	err := s.code.WalkTree(ctx, owner, repoName, ref, func(path string, size int64) error {
		// Skip if any ancestor directory is excluded.
		for dir := filepath.Dir(path); dir != "." && dir != "/" && dir != ""; dir = filepath.Dir(dir) {
			if excludedDirs[filepath.Base(dir)] {
				return nil
			}
		}
		ext := strings.ToLower(filepath.Ext(path))
		if lang, ok := extToLang[ext]; ok {
			comp[lang] += size
		}
		return nil
	})
	if err != nil {
		// Eclipses any prior success; intentional so a broken ref doesn't serve pre-breakage data.
		s.cache.Store(key, cacheEntry{err: err, cachedAt: time.Now()})
		return nil, err
	}
	s.cache.Store(key, cacheEntry{comp: comp, cachedAt: time.Now()})
	return comp, nil
}

// Drops every ref, not just the default branch: one push can move several.
func (s *LanguageService) InvalidateRepo(owner, repoName string) {
	prefix := owner + "/" + repoName + ":"
	s.cache.Range(func(k, _ any) bool {
		if strings.HasPrefix(k.(string), prefix) {
			s.cache.Delete(k)
		}
		return true
	})
}

// Percent is an integer; sum may be ≤ 100 due to rounding.
type LangPercent struct {
	Name    string
	Percent int
}

// Drops languages contributing less than 1%.
func (s *LanguageService) Percentages(ctx context.Context, owner, repoName, ref string) ([]LangPercent, error) {
	comp, err := s.Composition(ctx, owner, repoName, ref)
	if err != nil {
		return nil, err
	}
	return rankLanguages(comp, 0), nil
}

// TopLanguageFor returns the language with the largest byte count in the repo's
// default tree. Ties are broken by alphabetical order. Returns ("", nil) when
// no recognised code is found (including repos containing only empty source
// files or only excluded extensions like Markdown).
func (s *LanguageService) TopLanguageFor(ctx context.Context, owner, repoName, ref string) (string, error) {
	comp, err := s.Composition(ctx, owner, repoName, ref)
	if err != nil {
		return "", err
	}
	var top string
	var topBytes int64
	for name, b := range comp {
		if b > topBytes || (b == topBytes && name < top) {
			top = name
			topBytes = b
		}
	}
	return top, nil
}

// PrimaryLanguage prefers the column written at push time. Nil and "" both fall
// back to a cached tree walk, so a column a push left empty heals on view;
// README-only repos pay that walk.
func (s *LanguageService) PrimaryLanguage(ctx context.Context, repo *model.Repository) string {
	if repo.PrimaryLanguage != nil && *repo.PrimaryLanguage != "" {
		return *repo.PrimaryLanguage
	}
	lang, err := s.TopLanguageFor(ctx, repo.OwnerName, repo.Name, repo.DefaultBranch)
	if err != nil {
		return ""
	}
	return lang
}

// Only repos viewerID can read count, so private code never shapes a visitor's
// view. Per-repo failures (empty repo, bad ref) are skipped so one broken repo
// can't blank out the whole composition. limit <= 0 returns all languages.
func (s *LanguageService) AggregateForUser(ctx context.Context, username string, viewerID *int64, limit int) ([]LangPercent, error) {
	repos, err := s.repos.ListByOwnerVisibleTo(ctx, username, viewerID)
	if err != nil {
		return nil, err
	}
	totals := make(map[string]int64)
	for _, r := range repos {
		comp, err := s.Composition(ctx, r.OwnerName, r.Name, r.DefaultBranch)
		if err != nil {
			slog.WarnContext(ctx, "language_service: composition failed for repo",
				"owner", r.OwnerName, "name", r.Name, "ref", r.DefaultBranch, "err", err)
			continue
		}
		for name, b := range comp {
			totals[name] += b
		}
	}
	return rankLanguages(totals, limit), nil
}

// Counts each visible repo's cached primary language instead of walking every
// tree, so the org page stays one query.
func (s *LanguageService) AggregateForOrg(ctx context.Context, orgID int64, viewerID *int64, limit int) ([]LangPercent, error) {
	repos, err := s.orgs.ListReposVisibleTo(ctx, orgID, viewerID)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64)
	for _, r := range repos {
		if r.PrimaryLanguage != nil && *r.PrimaryLanguage != "" {
			counts[*r.PrimaryLanguage]++
		}
	}
	return rankLanguages(counts, limit), nil
}

// rankLanguages keeps the top limit languages by weight (all when limit <= 0)
// and computes percentages over the kept ones only, so a top-N bar fills to
// ~100% instead of leaving the dropped tail as a gap.
func rankLanguages(weights map[string]int64, limit int) []LangPercent {
	names := make([]string, 0, len(weights))
	for name, w := range weights {
		if w > 0 {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if weights[names[i]] != weights[names[j]] {
			return weights[names[i]] > weights[names[j]]
		}
		return names[i] < names[j]
	})
	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}
	var total int64
	for _, name := range names {
		total += weights[name]
	}
	out := make([]LangPercent, 0, len(names))
	for _, name := range names {
		if pct := int(weights[name] * 100 / total); pct > 0 {
			out = append(out, LangPercent{Name: name, Percent: pct})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Percent != out[j].Percent {
			return out[i].Percent > out[j].Percent
		}
		return out[i].Name < out[j].Name
	})
	return out
}
