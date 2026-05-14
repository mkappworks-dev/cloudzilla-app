package service

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// extToLang maps a file extension (lowercase, with leading dot) to its
// canonical language label. README/Markdown/configs/lockfiles are
// intentionally excluded — composition is about *code*.
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

// excludedDirs are skipped during the scan (vendored, build output, VCS).
var excludedDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"target":       true,
}

// LanguageService computes per-repo language composition by walking the
// repo tree at a given ref and bucketing files by extension. Results are
// cached for 10 minutes per (owner/repo, ref).
type LanguageService struct {
	code  *CodeService
	cache sync.Map // key="owner/repo:ref" → cacheEntry
}

type cacheEntry struct {
	comp     map[string]int64
	cachedAt time.Time
}

const langCacheTTL = 10 * time.Minute

// NewLanguageService returns a LanguageService that reads tree data via
// the given CodeService.
func NewLanguageService(code *CodeService) *LanguageService {
	return &LanguageService{code: code}
}

// Composition returns bytes-per-language for the repo at the given ref.
// Cached for 10 minutes per (owner/repo, ref).
func (s *LanguageService) Composition(ctx context.Context, owner, repoName, ref string) (map[string]int64, error) {
	key := owner + "/" + repoName + ":" + ref
	if v, ok := s.cache.Load(key); ok {
		e := v.(cacheEntry)
		if time.Since(e.cachedAt) < langCacheTTL {
			return e.comp, nil
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
		return nil, err
	}
	s.cache.Store(key, cacheEntry{comp: comp, cachedAt: time.Now()})
	return comp, nil
}

// LangPercent is one row of the normalized language composition: name +
// integer percent (sums to ≤ 100; rounding may drop a percentage point).
type LangPercent struct {
	Name    string
	Percent int
}

// Percentages returns Composition normalized to integer percentages,
// sorted descending. Languages contributing less than 1% are dropped.
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
