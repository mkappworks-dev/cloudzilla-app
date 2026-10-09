package gitref

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidName means a branch or tag name breaks the rules of
// `git check-ref-format`, so a git client could fail to fetch or clone it.
var ErrInvalidName = errors.New("invalid ref name")

// ValidateName checks a name as it sits under refs/heads/ or refs/tags/ against
// the rules of `git check-ref-format`. go-git's own check misses most of them.
func ValidateName(name string) error {
	if name == "" {
		return invalid(name, "it is empty")
	}
	if name == "@" {
		return invalid(name, "it can't be a lone @")
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return invalid(name, "it can't start or end with /")
	}
	if strings.HasSuffix(name, ".") {
		return invalid(name, "it can't end with .")
	}
	if strings.Contains(name, "..") {
		return invalid(name, "it can't contain ..")
	}
	if strings.Contains(name, "@{") {
		return invalid(name, "it can't contain @{")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\", r) {
			return invalid(name, fmt.Sprintf("it can't contain %q", r))
		}
	}
	for _, part := range strings.Split(name, "/") {
		switch {
		case part == "":
			return invalid(name, "it can't contain //")
		case strings.HasPrefix(part, "."):
			return invalid(name, "no part can start with .")
		case strings.HasSuffix(part, ".lock"):
			return invalid(name, "no part can end with .lock")
		}
	}
	return nil
}

// ValidateBranchName is ValidateName plus the two extra refusals git applies
// to branches: a leading - (it would read as an option) and HEAD.
func ValidateBranchName(name string) error {
	if strings.HasPrefix(name, "-") {
		return invalid(name, "a branch can't start with -")
	}
	if name == "HEAD" {
		return invalid(name, "a branch can't be named HEAD")
	}
	return ValidateName(name)
}

func invalid(name, why string) error {
	return fmt.Errorf("%w: %q: %s", ErrInvalidName, name, why)
}
