package workflow

import (
	"regexp"
	"strings"
)

// Event is what happened in a repository, as the run creator sees it.
type Event struct {
	// Name is push, pull_request or workflow_dispatch.
	Name string
	// Ref is the full ref for a push (refs/heads/x or refs/tags/x); it is unused otherwise.
	Ref string
	// Action is the pull request action: opened, reopened or synchronize.
	Action string
}

// Matches reports whether the workflow should run for e.
func (t Triggers) Matches(e Event) bool {
	switch e.Name {
	case "push":
		return t.Push != nil && t.Push.matches(e.Ref)
	case "pull_request":
		if t.PullRequest == nil {
			return false
		}
		for _, ty := range t.PullRequest.Types {
			if ty == e.Action {
				return true
			}
		}
	case "workflow_dispatch":
		return t.Dispatch
	}
	return false
}

func (p *PushTrigger) matches(ref string) bool {
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		if len(p.Branches) == 0 {
			return len(p.Tags) == 0
		}
		return matchPatterns(p.Branches, strings.TrimPrefix(ref, "refs/heads/"))
	case strings.HasPrefix(ref, "refs/tags/"):
		if len(p.Tags) == 0 {
			return len(p.Branches) == 0
		}
		return matchPatterns(p.Tags, strings.TrimPrefix(ref, "refs/tags/"))
	}
	return false
}

// matchPatterns applies patterns in order: a match includes, a matching !pattern excludes what came before.
func matchPatterns(patterns []string, name string) bool {
	matched := false
	for _, p := range patterns {
		if neg, ok := strings.CutPrefix(p, "!"); ok {
			if matched && globRegexp(neg).MatchString(name) {
				matched = false
			}
		} else if globRegexp(p).MatchString(name) {
			matched = true
		}
	}
	return matched
}

// globRegexp follows GitHub's filter patterns: * stops at /, ** does not, ? and + apply to the preceding character.
func globRegexp(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?' || c == '+':
			b.WriteByte(c)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile(`^\b$`)
	}
	return re
}
