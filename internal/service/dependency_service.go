package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// DependencyService parses and stores dependency manifests for the dependency graph.
type DependencyService struct {
	dep  *store.DependencyStore
	code *CodeService
}

// NewDependencyService creates a DependencyService backed by the given store and code service.
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
		{"pyproject.toml", parsePyprojectToml},
		{"Pipfile", parsePipfile},
		{"Cargo.toml", parseCargoToml},
	}

	var all []model.RepoDependency
	infraErr := false
	for _, m := range manifests {
		raw, err := s.code.GetRawBlob(repo.OwnerName, repo.Name, repo.DefaultBranch, m.path)
		if err != nil {
			if !errors.Is(err, ErrEmptyRepo) && !errors.Is(err, ErrRefNotFound) && !errors.Is(err, object.ErrFileNotFound) {
				slog.Warn("dependency: unexpected error reading manifest",
					"repo_id", repo.ID,
					"owner", repo.OwnerName,
					"repo", repo.Name,
					"manifest", m.path,
					"error", err,
				)
				infraErr = true
			}
			continue
		}
		parsed := m.parser(string(raw))
		all = append(all, parsed...)
	}

	// If infrastructure errors prevented all reads, skip the replace to avoid
	// wiping the stored dependency graph due to a transient failure.
	if infraErr && len(all) == 0 {
		return errors.New("dependency: manifest reads failed due to infrastructure errors; skipping replace")
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
		// Skip pip flags (-r, -c, -e, --find-links, etc.) and URL-based requirements
		if strings.HasPrefix(line, "-") || strings.Contains(line, "://") {
			continue
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

var poetryRhsVersionRe = regexp.MustCompile(`version\s*=\s*"([^"]+)"`)
var pyAssignRe = regexp.MustCompile(`^\s*([A-Za-z0-9_.\-]+)\s*=\s*(.+)$`)

// parsePyprojectToml extracts pip dependencies from a pyproject.toml file.
// Supports PEP 621 ([project] dependencies and [project.optional-dependencies])
// and Poetry ([tool.poetry.dependencies], [tool.poetry.dev-dependencies], and
// [tool.poetry.group.<name>.dependencies]).
func parsePyprojectToml(content string) []model.RepoDependency {
	var deps []model.RepoDependency

	section := ""
	inArray := false
	arrIsDev := false

	addPep508 := func(raw string, isDev bool) {
		s := strings.TrimSpace(raw)
		s = strings.TrimSuffix(s, ",")
		s = strings.TrimSpace(s)
		if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
			s = s[1 : len(s)-1]
		}
		if s == "" {
			return
		}
		pkg, ver := splitPep508(s)
		if pkg == "" {
			return
		}
		deps = append(deps, model.RepoDependency{
			PackageMgr: "pip",
			Package:    pkg,
			Version:    ver,
			IsDev:      isDev,
		})
	}

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if inArray {
			if idx := findArrayEnd(trimmed); idx != -1 {
				splitTomlArrayEntries(trimmed[:idx], arrIsDev, addPep508)
				inArray = false
				arrIsDev = false
				continue
			}
			splitTomlArrayEntries(trimmed, arrIsDev, addPep508)
			continue
		}

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		switch {
		case section == "[project]":
			rest, ok := stripAssign(trimmed, "dependencies")
			if !ok {
				continue
			}
			if !strings.HasPrefix(rest, "[") {
				continue
			}
			rest = rest[1:]
			if idx := findArrayEnd(rest); idx != -1 {
				splitTomlArrayEntries(rest[:idx], false, addPep508)
			} else {
				inArray = true
				arrIsDev = false
				splitTomlArrayEntries(rest, false, addPep508)
			}
		case section == "[project.optional-dependencies]":
			eq := strings.Index(trimmed, "=")
			if eq == -1 {
				continue
			}
			group := strings.TrimSpace(trimmed[:eq])
			rest := strings.TrimSpace(trimmed[eq+1:])
			if !strings.HasPrefix(rest, "[") {
				continue
			}
			rest = rest[1:]
			isDev := isDevGroup(group)
			if idx := findArrayEnd(rest); idx != -1 {
				splitTomlArrayEntries(rest[:idx], isDev, addPep508)
			} else {
				inArray = true
				arrIsDev = isDev
				splitTomlArrayEntries(rest, isDev, addPep508)
			}
		case section == "[tool.poetry.dependencies]",
			section == "[tool.poetry.dev-dependencies]":
			isDev := section == "[tool.poetry.dev-dependencies]"
			if d, ok := parsePoetryAssign(line, isDev); ok {
				deps = append(deps, d)
			}
		case strings.HasPrefix(section, "[tool.poetry.group.") &&
			strings.HasSuffix(section, ".dependencies]"):
			groupName := section[len("[tool.poetry.group.") : len(section)-len(".dependencies]")]
			if d, ok := parsePoetryAssign(line, isDevGroup(groupName)); ok {
				deps = append(deps, d)
			}
		}
	}
	return deps
}

// parsePipfile extracts pip dependencies from a Pipfile.
// Reads [packages] (runtime) and [dev-packages] (dev). The Pipfile wildcard
// "*" is normalized to an empty version string.
func parsePipfile(content string) []model.RepoDependency {
	var deps []model.RepoDependency
	section := ""

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
			continue
		}
		if section != "[packages]" && section != "[dev-packages]" {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := pyAssignRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ver := poetryRhsVersion(m[2])
		if ver == "*" {
			ver = ""
		}
		deps = append(deps, model.RepoDependency{
			PackageMgr: "pip",
			Package:    m[1],
			Version:    ver,
			IsDev:      section == "[dev-packages]",
		})
	}
	return deps
}

// stripAssign returns the right-hand side of `key = ...` if `trimmed` starts
// with `key` followed by `=`. Used to detect lines like `dependencies = [...]`.
func stripAssign(trimmed, key string) (string, bool) {
	if !strings.HasPrefix(trimmed, key) {
		return "", false
	}
	rest := strings.TrimSpace(trimmed[len(key):])
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	return strings.TrimSpace(rest[1:]), true
}

// findArrayEnd returns the index of the first `]` that lies outside any
// quoted string in s, or -1 if none is present. Used to detect the close of
// a multiline TOML array without misfiring on `]` inside a quoted PEP 508
// specifier such as "click[colors]".
func findArrayEnd(s string) int {
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case ']':
			return i
		}
	}
	return -1
}

// splitTomlArrayEntries splits the inside of a TOML array on commas that lie
// outside of quoted strings, then invokes addFn on each entry.
func splitTomlArrayEntries(s string, isDev bool, addFn func(string, bool)) {
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			cur.WriteByte(c)
			continue
		}
		if c == ',' {
			if e := strings.TrimSpace(cur.String()); e != "" {
				addFn(e, isDev)
			}
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	if e := strings.TrimSpace(cur.String()); e != "" {
		addFn(e, isDev)
	}
}

// splitPep508 extracts the package name and version from a PEP 508 dependency
// specifier such as `requests>=2.0,<3.0; python_version>="3.7"`.
func splitPep508(spec string) (pkg, ver string) {
	if idx := strings.Index(spec, ";"); idx != -1 {
		spec = strings.TrimSpace(spec[:idx])
	}
	if br := strings.Index(spec, "["); br != -1 {
		if cb := strings.Index(spec, "]"); cb > br {
			spec = spec[:br] + spec[cb+1:]
		}
	}
	spec = strings.TrimSpace(spec)
	bestIdx := -1
	bestLen := 0
	for _, op := range []string{"===", "==", ">=", "<=", "~=", "!=", ">", "<"} {
		idx := strings.Index(spec, op)
		if idx == -1 {
			continue
		}
		if bestIdx == -1 || idx < bestIdx || (idx == bestIdx && len(op) > bestLen) {
			bestIdx = idx
			bestLen = len(op)
		}
	}
	if bestIdx == -1 {
		return strings.TrimSpace(spec), ""
	}
	pkg = strings.TrimSpace(spec[:bestIdx])
	ver = strings.TrimSpace(spec[bestIdx+bestLen:])
	for _, op := range []string{"===", "==", ">=", "<=", "~=", "!=", ">", "<"} {
		if strings.HasPrefix(ver, op) {
			ver = strings.TrimSpace(ver[len(op):])
			break
		}
	}
	if comma := strings.Index(ver, ","); comma != -1 {
		ver = strings.TrimSpace(ver[:comma])
	}
	return
}

// parsePoetryAssign turns a single Poetry dependency assignment line into a
// RepoDependency. Returns ok=false for the special `python = "..."` runtime
// constraint or unparseable lines.
func parsePoetryAssign(line string, isDev bool) (model.RepoDependency, bool) {
	m := pyAssignRe.FindStringSubmatch(line)
	if m == nil {
		return model.RepoDependency{}, false
	}
	if m[1] == "python" {
		return model.RepoDependency{}, false
	}
	return model.RepoDependency{
		PackageMgr: "pip",
		Package:    m[1],
		Version:    poetryRhsVersion(m[2]),
		IsDev:      isDev,
	}, true
}

// poetryRhsVersion extracts a clean version string from the right-hand side of
// a Poetry/Pipfile dependency line. Handles `"x.y"` and `{ version = "x.y", ... }`,
// and strips Poetry's range operators (^, ~, >=, etc.).
func poetryRhsVersion(rhs string) string {
	rhs = strings.TrimSpace(rhs)
	if idx := strings.Index(rhs, "#"); idx != -1 {
		rhs = strings.TrimSpace(rhs[:idx])
	}
	if len(rhs) > 0 && (rhs[0] == '"' || rhs[0] == '\'') {
		q := rhs[0]
		if end := strings.IndexByte(rhs[1:], q); end >= 0 {
			return strings.TrimLeft(rhs[1:1+end], "^~>=<!")
		}
	}
	if strings.HasPrefix(rhs, "{") {
		if m := poetryRhsVersionRe.FindStringSubmatch(rhs); m != nil {
			return strings.TrimLeft(m[1], "^~>=<!")
		}
	}
	return ""
}

func isDevGroup(name string) bool {
	switch strings.ToLower(name) {
	case "dev", "test", "tests", "testing", "lint", "linting", "type", "typing":
		return true
	}
	return false
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
