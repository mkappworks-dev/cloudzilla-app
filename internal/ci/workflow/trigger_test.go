package workflow

import "testing"

func triggersFor(t *testing.T, on string) Triggers {
	t.Helper()
	return mustParse(t, on+"jobs:\n  a:\n    runs-on: x\n    steps:\n      - run: y\n").On
}

func TestPushBranchAndTagMatching(t *testing.T) {
	tests := []struct {
		name, on string
		ref      string
		want     bool
	}{
		{"no filters match a branch", "on: push\n", "refs/heads/anything", true},
		{"no filters match a tag", "on: push\n", "refs/tags/v1", true},
		{"literal branch", "on:\n  push:\n    branches: [main]\n", "refs/heads/main", true},
		{"literal branch miss", "on:\n  push:\n    branches: [main]\n", "refs/heads/dev", false},
		{"branches only ignores tags", "on:\n  push:\n    branches: [main]\n", "refs/tags/main", false},
		{"tags only ignores branches", "on:\n  push:\n    tags: ['v*']\n", "refs/heads/v1", false},
		{"tag glob", "on:\n  push:\n    tags: ['v*']\n", "refs/tags/v1.2", true},
		{"star stops at slash", "on:\n  push:\n    branches: ['feat/*']\n", "refs/heads/feat/a", true},
		{"star does not cross slash", "on:\n  push:\n    branches: ['feat/*']\n", "refs/heads/feat/a/b", false},
		{"double star crosses slash", "on:\n  push:\n    branches: ['feat/**']\n", "refs/heads/feat/a/b", true},
		{"double star matches whole name", "on:\n  push:\n    branches: ['**']\n", "refs/heads/a/b/c", true},
		{"single star alone skips slash refs", "on:\n  push:\n    branches: ['*']\n", "refs/heads/release/1", false},
		{"ref with a slash literal", "on:\n  push:\n    branches: ['release/1.x']\n", "refs/heads/release/1.x", true},
		{"dot is literal", "on:\n  push:\n    branches: ['v1.x']\n", "refs/heads/v1ax", false},
		{"negation excludes", "on:\n  push:\n    branches: ['release/**', '!release/old/**']\n", "refs/heads/release/old/1", false},
		{"negation keeps others", "on:\n  push:\n    branches: ['release/**', '!release/old/**']\n", "refs/heads/release/2", true},
		{"later positive re-includes", "on:\n  push:\n    branches: ['a/**', '!a/b', 'a/b']\n", "refs/heads/a/b", true},
		{"branches and tags either", "on:\n  push:\n    branches: [main]\n    tags: ['v*']\n", "refs/tags/v2", true},
		{"plus quantifier", "on:\n  push:\n    branches: ['v1+']\n", "refs/heads/v111", true},
		{"unrelated ref", "on: push\n", "refs/pull/1/head", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := triggersFor(t, tt.on).Matches(Event{Name: "push", Ref: tt.ref}); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOnlyNegativePatternsRejected(t *testing.T) {
	e := firstError(t, "on:\n  push:\n    branches: ['!main']\njobs: {}\n")
	if e.Line != 3 {
		t.Errorf("got %v", e)
	}
}

func TestPullRequestAndDispatchMatching(t *testing.T) {
	pr := triggersFor(t, "on: pull_request\n")
	for action, want := range map[string]bool{"opened": true, "reopened": true, "synchronize": true, "closed": false} {
		if got := pr.Matches(Event{Name: "pull_request", Action: action}); got != want {
			t.Errorf("%s: got %v", action, got)
		}
	}
	narrow := triggersFor(t, "on:\n  pull_request:\n    types: [opened]\n")
	if narrow.Matches(Event{Name: "pull_request", Action: "synchronize"}) || !narrow.Matches(Event{Name: "pull_request", Action: "opened"}) {
		t.Error("types filter")
	}
	if pr.Matches(Event{Name: "push", Ref: "refs/heads/x"}) {
		t.Error("a pull_request-only workflow ignores pushes")
	}
	if pr.Matches(Event{Name: "workflow_dispatch"}) {
		t.Error("dispatch must be declared")
	}
	d := triggersFor(t, "on:\n  workflow_dispatch:\n")
	if !d.Matches(Event{Name: "workflow_dispatch", Ref: "refs/heads/any"}) {
		t.Error("dispatch matches")
	}
}
