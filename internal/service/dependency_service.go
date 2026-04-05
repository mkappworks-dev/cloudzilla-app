package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
)

type DependencyService struct {
	dep  *store.DependencyStore
	code *CodeService
}

func NewDependencyService(dep *store.DependencyStore, code *CodeService) *DependencyService {
	return &DependencyService{dep: dep, code: code}
}

// ParseAndStore reads known manifest files from the repo's default branch,
// parses them, and replaces the stored dependency list.
func (s *DependencyService) ParseAndStore(ctx context.Context, repo *model.Repository) error {
	type manifest struct {
		path   string
		parser func(string) []model.RepoDependency
	}
	manifests := []manifest{
		{"go.mod", parseGoMod},
		{"package.json", parsePackageJSON},
		{"requirements.txt", parseRequirementsTxt},
		{"Cargo.toml", parseCargoToml},
	}

	var all []model.RepoDependency
	for _, m := range manifests {
		raw, err := s.code.GetRawBlob(repo.OwnerName, repo.Name, repo.DefaultBranch, m.path)
		if err != nil {
			if !errors.Is(err, ErrEmptyRepo) && !errors.Is(err, object.ErrFileNotFound) {
				slog.Warn("dependency: unexpected error reading manifest",
					"repo_id", repo.ID,
					"owner", repo.OwnerName,
					"repo", repo.Name,
					"manifest", m.path,
					"error", err,
				)
			}
			continue
		}
		parsed := m.parser(string(raw))
		all = append(all, parsed...)
	}

	return s.dep.Replace(ctx, repo.ID, all)
}

// ListByRepo returns all stored dependencies for a repo.
func (s *DependencyService) ListByRepo(ctx context.Context, repoID int64) ([]model.RepoDependency, error) {
	return s.dep.ListByRepo(ctx, repoID)
}

// ---------------------------------------------------------------------------
// Parsers
// ---------------------------------------------------------------------------

var goRequireLineRe = regexp.MustCompile(`^\s+(\S+)\s+(\S+)`)
var goRequireSingleRe = regexp.MustCompile(`^require\s+(\S+)\s+(\S+)`)

// parseGoMod extracts dependencies from a go.mod file content.
func parseGoMod(content string) []model.RepoDependency {
	var deps []model.RepoDependency
	inRequireBlock := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "require (" {
			inRequireBlock = true
			continue
		}
		if inRequireBlock && trimmed == ")" {
			inRequireBlock = false
			continue
		}

		if inRequireBlock {
			m := goRequireLineRe.FindStringSubmatch(line)
			if m != nil && !strings.HasPrefix(trimmed, "//") {
				deps = append(deps, model.RepoDependency{
					PackageMgr: "go",
					Package:    m[1],
					Version:    m[2],
					IsDev:      false,
				})
			}
			continue
		}

		// Single-line require outside a block
		m := goRequireSingleRe.FindStringSubmatch(trimmed)
		if m != nil {
			deps = append(deps, model.RepoDependency{
				PackageMgr: "go",
				Package:    m[1],
				Version:    m[2],
				IsDev:      false,
			})
		}
	}
	return deps
}

// parsePackageJSON extracts npm dependencies from a package.json file content.
func parsePackageJSON(content string) []model.RepoDependency {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(content), &pkg); err != nil {
		slog.Warn("dependency: failed to parse package.json", "error", err)
		return nil
	}
	var deps []model.RepoDependency
	for name, ver := range pkg.Dependencies {
		deps = append(deps, model.RepoDependency{
			PackageMgr: "npm",
			Package:    name,
			Version:    strings.TrimLeft(ver, "^~>="),
			IsDev:      false,
		})
	}
	for name, ver := range pkg.DevDependencies {
		deps = append(deps, model.RepoDependency{
			PackageMgr: "npm",
			Package:    name,
			Version:    strings.TrimLeft(ver, "^~>="),
			IsDev:      true,
		})
	}
	return deps
}

// parseRequirementsTxt extracts pip dependencies from a requirements.txt file content.
func parseRequirementsTxt(content string) []model.RepoDependency {
	var deps []model.RepoDependency
	operators := []string{"==", ">=", "<=", "~=", "!=", ">", "<"}

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip inline comments
		if idx := strings.Index(line, " #"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		pkg := line
		ver := ""
		for _, op := range operators {
			if idx := strings.Index(line, op); idx != -1 {
				pkg = strings.TrimSpace(line[:idx])
				ver = strings.TrimSpace(line[idx+len(op):])
				// Strip any further constraint (e.g. ",<3.0")
				if comma := strings.Index(ver, ","); comma != -1 {
					ver = ver[:comma]
				}
				break
			}
		}
		if pkg == "" {
			continue
		}
		deps = append(deps, model.RepoDependency{
			PackageMgr: "pip",
			Package:    pkg,
			Version:    ver,
			IsDev:      false,
		})
	}
	return deps
}

// parseCargoToml extracts Rust crate dependencies from a Cargo.toml file content.
func parseCargoToml(content string) []model.RepoDependency {
	var deps []model.RepoDependency
	inDeps := false
	inDevDeps := false

	versionRe := regexp.MustCompile(`version\s*=\s*"([^"]+)"`)
	simpleVerRe := regexp.MustCompile(`^\s*(\S+)\s*=\s*"([^"]+)"`)
	tableVerRe := regexp.MustCompile(`^\s*(\S+)\s*=\s*\{`)

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "[dependencies]" {
			inDeps = true
			inDevDeps = false
			continue
		}
		if trimmed == "[dev-dependencies]" {
			inDevDeps = true
			inDeps = false
			continue
		}
		// Any other [section] header ends the dep blocks
		if strings.HasPrefix(trimmed, "[") {
			if trimmed != "[dependencies]" && trimmed != "[dev-dependencies]" {
				inDeps = false
				inDevDeps = false
			}
			continue
		}

		if !inDeps && !inDevDeps {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		isDev := inDevDeps

		// Simple: name = "version"
		if m := simpleVerRe.FindStringSubmatch(line); m != nil {
			deps = append(deps, model.RepoDependency{
				PackageMgr: "cargo",
				Package:    m[1],
				Version:    m[2],
				IsDev:      isDev,
			})
			continue
		}

		// Table: name = { version = "...", ... }
		if m := tableVerRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			ver := ""
			if vm := versionRe.FindStringSubmatch(line); vm != nil {
				ver = vm[1]
			}
			deps = append(deps, model.RepoDependency{
				PackageMgr: "cargo",
				Package:    name,
				Version:    ver,
				IsDev:      isDev,
			})
			continue
		}
	}
	return deps
}
