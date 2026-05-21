package service

import (
	"embed"
	"strconv"
	"strings"
	"time"
)

//go:embed repotemplates/gitignore/*.gitignore repotemplates/licenses/*.txt
var repoTemplateFS embed.FS

// License describes a license template offered on the new-repo form.
type License struct {
	Key  string // embed file basename, e.g. "mit"
	Name string // display name, e.g. "MIT License"
}

var gitignoreTemplates = []string{"Go", "Node", "Python", "Rust", "Java", "C++", "Ruby"}

var licenseTemplates = []License{
	{Key: "mit", Name: "MIT License"},
}

// gitignoreFileNames maps a template name to its embed file basename.
// "C++" cannot be a clean embed path segment, so it ships as "Cpp.gitignore".
var gitignoreFileNames = map[string]string{
	"Go":     "Go",
	"Node":   "Node",
	"Python": "Python",
	"Rust":   "Rust",
	"Java":   "Java",
	"C++":    "Cpp",
	"Ruby":   "Ruby",
}

// ListGitignoreTemplates returns the available .gitignore template names.
func (s *RepoService) ListGitignoreTemplates() []string { return gitignoreTemplates }

// ListLicenseTemplates returns the available license templates.
func (s *RepoService) ListLicenseTemplates() []License { return licenseTemplates }

// gitignoreContent returns the .gitignore body for the named template.
// Returns ("", false) for an unknown or empty name.
func gitignoreContent(name string) (string, bool) {
	file, ok := gitignoreFileNames[name]
	if !ok {
		return "", false
	}
	b, err := repoTemplateFS.ReadFile("repotemplates/gitignore/" + file + ".gitignore")
	if err != nil {
		return "", false
	}
	return string(b), true
}

// licenseContent returns the license body for the given key with the [year]
// and [fullname] placeholders substituted. Returns ("", false) for an unknown
// or empty key.
func licenseContent(key, ownerName string) (string, bool) {
	known := false
	for _, l := range licenseTemplates {
		if l.Key == key {
			known = true
			break
		}
	}
	if !known {
		return "", false
	}
	b, err := repoTemplateFS.ReadFile("repotemplates/licenses/" + key + ".txt")
	if err != nil {
		return "", false
	}
	text := string(b)
	text = strings.ReplaceAll(text, "[year]", strconv.Itoa(time.Now().Year()))
	text = strings.ReplaceAll(text, "[fullname]", ownerName)
	return text, true
}
