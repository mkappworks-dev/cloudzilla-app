package service

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
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
	repos *store.RepoStore
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

func NewLanguageService(code *CodeService, repos *store.RepoStore) *LanguageService {
	return &LanguageService{code: code, repos: repos}
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
	var total int64
	for _, b := range comp {
		total += b
	}
	if total == 0 {
		return nil, nil
	}
	out := make([]LangPercent, 0, len(comp))
	for name, b := range comp {
		pct := int(b * 100 / total)
		if pct > 0 {
			out = append(out, LangPercent{Name: name, Percent: pct})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Percent != out[j].Percent {
			return out[i].Percent > out[j].Percent
		}
		return out[i].Name < out[j].Name // stable tiebreak
	})
	return out, nil
}

// Returns "" when the repo has no detected source files.
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

// Per-repo failures (empty repo, bad ref) are skipped so a single broken repo
// can't blank out the user's whole composition.
func (s *LanguageService) AggregateForUser(ctx context.Context, userID int64, limit int) ([]LangPercent, error) {
	repos, err := s.repos.GetByOwnerID(ctx, userID)
	if err != nil {
		return nil, err
	}
	totals := make(map[string]int64)
	for _, r := range repos {
		comp, err := s.Composition(ctx, r.OwnerName, r.Name, r.DefaultBranch)
		if err != nil {
			continue
		}
		for name, b := range comp {
			totals[name] += b
		}
	}
	var total int64
	for _, b := range totals {
		total += b
	}
	if total == 0 {
		return nil, nil
	}
	out := make([]LangPercent, 0, len(totals))
	for name, b := range totals {
		pct := int(b * 100 / total)
		if pct > 0 {
			out = append(out, LangPercent{Name: name, Percent: pct})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Percent != out[j].Percent {
			return out[i].Percent > out[j].Percent
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
