package store

import (
	"strconv"
	"strings"
)

// ListFilter narrows and pages a repo's issue or pull request list. Names match case-insensitively.
type ListFilter struct {
	Text        string
	Author      string
	Assignee    string
	Labels      []string // all must match
	Milestone   string   // title
	MilestoneID int64
	No          []string // "label", "milestone", "assignee"
	// Sort is "oldest", "updated" or "comments"; anything else is newest first.
	Sort string
	// State is "open"/"closed" for issues. For pulls it is "open" (non-draft), "draft", "merged" or "closed".
	State string
	// Review is a pull request review state: "none", "required", "approved" or "changes_requested".
	Review string
	// Reviewer is a username with a pending review request; pull requests only.
	Reviewer string
	// Mentions is a username mentioned in a comment.
	Mentions string
	// Repo is "owner/name". Only the account list queries, which join repositories as "r", honour it.
	Repo string
	// ReviewRequired is the approvals each base branch needs, from its protection rule; absent means none.
	ReviewRequired map[string]int
	// NoMatch short-circuits to an empty result, for a qualifier that cannot match, such as @me when signed out.
	NoMatch bool
	// Limit 0 means no limit.
	Limit, Offset int
}

type listTable struct {
	alias, labelTable, assigneeTable, fk, defaultOrder string
}

var (
	issueListTable = listTable{"i", "issue_labels", "issue_assignees", "issue_id", "i.created_at DESC, i.number DESC"}
	pullListTable  = listTable{"pr", "pull_labels", "pull_assignees", "pull_id", "pr.number DESC"}
)

type listBuilder struct {
	args []any
}

func (b *listBuilder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

// where returns the filter's predicates, ANDed by the caller. The table's users row must be aliased "u".
func (f ListFilter) where(t listTable, b *listBuilder) []string {
	a := t.alias
	var w []string
	if f.NoMatch {
		return []string{"FALSE"}
	}
	if f.Text != "" {
		w = append(w, a+`.title ILIKE `+b.arg("%"+escapeLike(f.Text)+"%")+` ESCAPE '\'`)
	}
	if f.Author != "" {
		w = append(w, `LOWER(u.username) = LOWER(`+b.arg(f.Author)+`)`)
	}
	if f.Assignee != "" {
		w = append(w, `EXISTS (SELECT 1 FROM `+t.assigneeTable+` fa JOIN users au ON au.id = fa.user_id
			WHERE fa.`+t.fk+` = `+a+`.id AND LOWER(au.username) = LOWER(`+b.arg(f.Assignee)+`))`)
	}
	for _, l := range f.Labels {
		w = append(w, `EXISTS (SELECT 1 FROM `+t.labelTable+` fl JOIN labels lb ON lb.id = fl.label_id
			WHERE fl.`+t.fk+` = `+a+`.id AND LOWER(lb.name) = LOWER(`+b.arg(l)+`))`)
	}
	if f.Milestone != "" {
		w = append(w, `EXISTS (SELECT 1 FROM milestones fm WHERE fm.id = `+a+`.milestone_id AND LOWER(fm.title) = LOWER(`+b.arg(f.Milestone)+`))`)
	}
	if f.MilestoneID != 0 {
		w = append(w, a+`.milestone_id = `+b.arg(f.MilestoneID))
	}
	if f.Review != "" && t == pullListTable {
		// Review state means something only while a PR can still be reviewed.
		w = append(w, "pr.state = 'open' AND NOT pr.is_draft AND "+reviewStateExpr(b, f.ReviewRequired)+" = "+b.arg(f.Review))
	}
	if f.Reviewer != "" && t == pullListTable {
		w = append(w, `EXISTS (SELECT 1 FROM pull_reviews rr JOIN users ru ON ru.id = rr.author_id
			WHERE rr.pull_id = pr.id AND rr.state = 'pending' AND LOWER(ru.username) = LOWER(`+b.arg(f.Reviewer)+`))`)
	}
	if f.Mentions != "" {
		w = append(w, `EXISTS (SELECT 1 FROM mentions mn JOIN comments mc ON mc.id = mn.comment_id JOIN users mu ON mu.id = mn.user_id
			WHERE mc.`+t.fk+` = `+a+`.id AND LOWER(mu.username) = LOWER(`+b.arg(f.Mentions)+`))`)
	}
	if f.Repo != "" {
		w = append(w, `LOWER(r.owner_name || '/' || r.name) = LOWER(`+b.arg(f.Repo)+`)`)
	}
	for _, n := range f.No {
		switch n {
		case "label":
			w = append(w, `NOT EXISTS (SELECT 1 FROM `+t.labelTable+` nl WHERE nl.`+t.fk+` = `+a+`.id)`)
		case "assignee":
			w = append(w, `NOT EXISTS (SELECT 1 FROM `+t.assigneeTable+` na WHERE na.`+t.fk+` = `+a+`.id)`)
		case "milestone":
			w = append(w, a+`.milestone_id IS NULL`)
		}
	}
	return w
}

func (f ListFilter) orderBy(t listTable) string {
	a := t.alias
	switch f.Sort {
	case "oldest":
		return a + ".created_at ASC, " + a + ".number ASC"
	case "updated":
		return a + ".updated_at DESC, " + a + ".number DESC"
	case "comments":
		// A per-row count before LIMIT: accepted, as sorting by comments is rare and idx_comments_* serves each lookup.
		return `(SELECT COUNT(*) FROM comments sc WHERE sc.` + t.fk + ` = ` + a + `.id) DESC, ` + a + ".number DESC"
	}
	return t.defaultOrder
}

// escapeLike makes s literal inside a LIKE pattern whose escape character is a backslash.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
