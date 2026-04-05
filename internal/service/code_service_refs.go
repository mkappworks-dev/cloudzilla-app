package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/mkappworks/cloudzilla/internal/model"
)

// IssueTemplate holds a parsed issue template file.
type IssueTemplate struct {
	Slug string // URL-safe key: filename without .md extension
	Name string // Display name: slug with dashes/underscores replaced by spaces
	Body string
}

// templateName converts a filename to a display name.
// "bug_report.md" → "bug report", "feature-request.md" → "feature request"
func templateName(filename string) string {
	name := strings.TrimSuffix(filename, ".md")
	if name == filename {
		return name // no .md suffix — return as-is
	}
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return name
}

// ListRefs returns all branches and tags for a repository.
func (s *CodeService) ListRefs(owner, repoName, defaultBranch string) (*RefsResult, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}

	var branches []BranchInfo
	branchIter, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	_ = branchIter.ForEach(func(ref *plumbing.Reference) error {
		hash := ref.Hash().String()
		if len(hash) > 7 {
			hash = hash[:7]
		}
		branches = append(branches, BranchInfo{
			Name:      ref.Name().Short(),
			Hash:      hash,
			IsDefault: ref.Name().Short() == defaultBranch,
		})
		return nil
	})
	sort.Slice(branches, func(i, j int) bool { return branches[i].Name < branches[j].Name })

	var tags []TagInfo
	tagIter, err := repo.Tags()
	if err != nil {
		return nil, err
	}
	_ = tagIter.ForEach(func(ref *plumbing.Reference) error {
		hash := ref.Hash().String()
		if len(hash) > 7 {
			hash = hash[:7]
		}
		tags = append(tags, TagInfo{
			Name: ref.Name().Short(),
			Hash: hash,
		})
		return nil
	})
	sort.Slice(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })

	return &RefsResult{Branches: branches, Tags: tags}, nil
}

// GetIssueTemplates reads .github/ISSUE_TEMPLATE/*.md from the default branch.
// Falls back to .github/ISSUE_TEMPLATE.md if the directory is absent.
// Returns nil (not an error) if no templates exist.
func (s *CodeService) GetIssueTemplates(owner, repoName, defaultBranch string) ([]IssueTemplate, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, _, err := resolveRef(repo, defaultBranch)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	// Try .github/ISSUE_TEMPLATE/ directory first
	if dirTree, err := tree.Tree(".github/ISSUE_TEMPLATE"); err == nil {
		var templates []IssueTemplate
		for _, entry := range dirTree.Entries {
			if entry.Mode != filemode.Regular && entry.Mode != filemode.Executable {
				continue
			}
			if !strings.HasSuffix(entry.Name, ".md") {
				continue
			}
			f, ferr := dirTree.File(entry.Name)
			if ferr != nil {
				continue
			}
			body, berr := f.Contents()
			if berr != nil {
				continue
			}
			slug := strings.TrimSuffix(entry.Name, ".md")
			templates = append(templates, IssueTemplate{Slug: slug, Name: templateName(entry.Name), Body: body})
		}
		if len(templates) > 0 {
			return templates, nil
		}
	}

	// Fall back to single .github/ISSUE_TEMPLATE.md
	if f, err := tree.File(".github/ISSUE_TEMPLATE.md"); err == nil {
		body, berr := f.Contents()
		if berr != nil {
			return nil, berr
		}
		return []IssueTemplate{{Slug: "issue", Name: "Issue", Body: body}}, nil
	}
	return nil, nil
}

// GetPRTemplate reads .github/PULL_REQUEST_TEMPLATE.md from the default branch.
// Returns "" (not an error) when the file does not exist.
func (s *CodeService) GetPRTemplate(owner, repoName, defaultBranch string) (string, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return "", err
	}
	commit, _, err := resolveRef(repo, defaultBranch)
	if err != nil {
		return "", err
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", err
	}
	f, err := tree.File(".github/PULL_REQUEST_TEMPLATE.md")
	if err != nil {
		return "", nil // file absent — not an error
	}
	return f.Contents()
}

// CreateBranch creates a new branch pointing to the resolved fromRef commit.
func (s *CodeService) CreateBranch(owner, repoName, name, fromRef string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	if _, err := repo.Reference(plumbing.NewBranchReferenceName(name), true); err == nil {
		return errors.New("branch already exists: " + name)
	}
	commit, _, err := resolveRef(repo, fromRef)
	if err != nil {
		return fmt.Errorf("from ref not found: %w", err)
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), commit.Hash)
	return repo.Storer.SetReference(ref)
}

// DeleteBranch removes the named branch reference.
func (s *CodeService) DeleteBranch(owner, repoName, name string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	return repo.Storer.RemoveReference(plumbing.NewBranchReferenceName(name))
}

// CreateTag creates a new lightweight tag pointing to the resolved fromRef commit.
func (s *CodeService) CreateTag(owner, repoName, name, fromRef string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	if _, err := repo.Reference(plumbing.NewTagReferenceName(name), true); err == nil {
		return errors.New("tag already exists: " + name)
	}
	commit, _, err := resolveRef(repo, fromRef)
	if err != nil {
		return fmt.Errorf("from ref not found: %w", err)
	}
	ref := plumbing.NewHashReference(plumbing.NewTagReferenceName(name), commit.Hash)
	return repo.Storer.SetReference(ref)
}

// DeleteTag removes the named tag reference.
func (s *CodeService) DeleteTag(owner, repoName, name string) error {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return err
	}
	return repo.Storer.RemoveReference(plumbing.NewTagReferenceName(name))
}

// GetCodeOwners reads the CODEOWNERS file from the default branch and returns parsed rules.
// Returns an empty slice (not an error) when no CODEOWNERS file exists.
func (s *CodeService) GetCodeOwners(owner, repoName, defaultBranch string) ([]model.CodeOwnerRule, error) {
	var raw []byte
	var err error
	for _, path := range []string{"CODEOWNERS", ".github/CODEOWNERS"} {
		raw, err = s.GetRawBlob(owner, repoName, defaultBranch, path)
		if err == nil {
			break
		}
	}
	if raw == nil {
		return nil, nil
	}
	var rules []model.CodeOwnerRule
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		rule := model.CodeOwnerRule{Pattern: fields[0]}
		for _, owner := range fields[1:] {
			rule.Owners = append(rule.Owners, strings.TrimPrefix(owner, "@"))
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// MatchCodeOwners returns a de-duplicated list of owner usernames whose patterns
// match any of the given changed files.
func (s *CodeService) MatchCodeOwners(rules []model.CodeOwnerRule, changedFiles []string) []string {
	seen := make(map[string]bool)
	var owners []string
	for _, rule := range rules {
		for _, file := range changedFiles {
			matched, err := filepath.Match(rule.Pattern, file)
			if err != nil {
				continue
			}
			if matched {
				for _, o := range rule.Owners {
					if !seen[o] {
						seen[o] = true
						owners = append(owners, o)
					}
				}
				break
			}
		}
	}
	return owners
}
