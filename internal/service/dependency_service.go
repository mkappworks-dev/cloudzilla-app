package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type DependencyService struct {
	dep  *store.DependencyStore
	code *CodeService
}

func NewDependencyService(dep *store.DependencyStore, code *CodeService) *DependencyService {
	return &DependencyService{dep: dep, code: code}
}

const maxManifestSize int64 = 1 << 20 // 1 MiB

func (s *DependencyService) ParseAndStore(ctx context.Context, repo *model.Repository) error {
	type manifest struct {
		path   string
		parser func(string) ([]model.RepoDependency, error)
	}
	manifests := []manifest{
		{"go.mod", parseGoModSafe},
		{"package.json", parsePackageJSONSafe},
		{"requirements.txt", parseRequirementsTxtSafe},
		{"pyproject.toml", parsePyprojectToml},
		{"Pipfile", parsePipfile},
		{"Cargo.toml", parseCargoTomlSafe},
	}

	var all []model.RepoDependency
	infraErr := false
	for _, m := range manifests {
		raw, err := s.code.GetRawBlobBounded(repo.OwnerName, repo.Name, repo.DefaultBranch, m.path, maxManifestSize)
		if err != nil {
			switch {
			case errors.Is(err, ErrEmptyRepo), errors.Is(err, ErrRefNotFound), errors.Is(err, object.ErrFileNotFound):
			case errors.Is(err, ErrBlobTooLarge):
				slog.Warn("dependency: manifest exceeds size limit; skipping",
					"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name,
					"manifest", m.path, "limit_bytes", maxManifestSize)
			default:
				slog.Warn("dependency: unexpected error reading manifest",
					"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name,
					"manifest", m.path, "error", err)
				infraErr = true
			}
			continue
		}
		parsed, perr := m.parser(string(raw))
		if perr != nil {
			slog.Warn("dependency: manifest parse failed; skipping",
				"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name,
				"manifest", m.path, "error", perr)
			continue
		}
		all = append(all, parsed...)
	}

	if infraErr && len(all) == 0 {
		return errors.New("dependency: manifest reads failed due to infrastructure errors; skipping replace")
	}

	return s.dep.Replace(ctx, repo.ID, all)
}

func parseGoModSafe(c string) ([]model.RepoDependency, error) { return parseGoMod(c), nil }
func parsePackageJSONSafe(c string) ([]model.RepoDependency, error) {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(c), &pkg); err != nil {
		return nil, fmt.Errorf("%w: package.json: %v", ErrMalformedManifest, err)
	}
	var deps []model.RepoDependency
	for name, ver := range pkg.Dependencies {
		deps = append(deps, model.RepoDependency{
			PackageMgr: "npm", Package: name,
			Version: strings.TrimLeft(ver, "^~>="), IsDev: false,
		})
	}
	for name, ver := range pkg.DevDependencies {
		deps = append(deps, model.RepoDependency{
			PackageMgr: "npm", Package: name,
			Version: strings.TrimLeft(ver, "^~>="), IsDev: true,
		})
	}
	return deps, nil
}
func parseRequirementsTxtSafe(c string) ([]model.RepoDependency, error) {
	return parseRequirementsTxt(c), nil
}
func parseCargoTomlSafe(c string) ([]model.RepoDependency, error) { return parseCargoToml(c), nil }

func (s *DependencyService) ListByRepo(ctx context.Context, repoID int64) ([]model.RepoDependency, error) {
	return s.dep.ListByRepo(ctx, repoID)
}

var goRequireLineRe = regexp.MustCompile(`^\s+(\S+)\s+(\S+)`)
var goRequireSingleRe = regexp.MustCompile(`^require\s+(\S+)\s+(\S+)`)

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

func parseRequirementsTxt(content string) []model.RepoDependency {
	var deps []model.RepoDependency
	operators := []string{"==", ">=", "<=", "~=", "!=", ">", "<"}

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, " #"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if strings.HasPrefix(line, "-") || strings.Contains(line, "://") {
			continue
		}
		pkg := line
		ver := ""
		for _, op := range operators {
			if idx := strings.Index(line, op); idx != -1 {
				pkg = strings.TrimSpace(line[:idx])
				ver = strings.TrimSpace(line[idx+len(op):])
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

var ErrMalformedManifest = errors.New("malformed manifest")

func parsePyprojectToml(content string) ([]model.RepoDependency, error) {
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
		trimmed := stripTomlComment(line)

		if inArray {
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && !strings.ContainsAny(trimmed, "\"'") {
				return nil, fmt.Errorf("%w: unterminated dependencies array before %s", ErrMalformedManifest, trimmed)
			}
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
		if trimmed == "" {
			continue
		}

		switch {
		case section == "[project]":
			rest, ok := stripExactAssign(trimmed, "dependencies")
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
			if group == "" || !strings.HasPrefix(rest, "[") {
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
	if inArray {
		return nil, fmt.Errorf("%w: unterminated dependencies array", ErrMalformedManifest)
	}
	return deps, nil
}

func stripTomlComment(line string) string {
	line = strings.TrimSpace(line)
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '#' {
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}

func stripExactAssign(trimmed, key string) (string, bool) {
	if !strings.HasPrefix(trimmed, key) {
		return "", false
	}
	rest := trimmed[len(key):]
	if rest == "" {
		return "", false
	}
	if c := rest[0]; c != ' ' && c != '\t' && c != '=' {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	return strings.TrimSpace(rest[1:]), true
}

func parsePipfile(content string) ([]model.RepoDependency, error) {
	var deps []model.RepoDependency
	section := ""
	inInlineTable := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if inInlineTable {
				return nil, fmt.Errorf("%w: unterminated inline table before %s", ErrMalformedManifest, trimmed)
			}
			section = trimmed
			continue
		}
		if section != "[packages]" && section != "[dev-packages]" {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if inInlineTable {
			if strings.HasSuffix(trimmed, "}") {
				inInlineTable = false
			}
			continue
		}
		m := pyAssignRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rhs := strings.TrimSpace(m[2])
		if idx := strings.Index(rhs, "#"); idx != -1 {
			rhs = strings.TrimSpace(rhs[:idx])
		}
		if strings.HasPrefix(rhs, "{") && !strings.HasSuffix(rhs, "}") {
			inInlineTable = true
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
	if inInlineTable {
		return nil, fmt.Errorf("%w: unterminated inline table at EOF", ErrMalformedManifest)
	}
	return deps, nil
}

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

		if m := simpleVerRe.FindStringSubmatch(line); m != nil {
			deps = append(deps, model.RepoDependency{
				PackageMgr: "cargo",
				Package:    m[1],
				Version:    m[2],
				IsDev:      isDev,
			})
			continue
		}

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
