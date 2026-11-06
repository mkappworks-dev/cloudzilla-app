# Issue & PR list filters — mock

Make the repo Issues and Pull requests lists filterable, sortable and paged in the same way. The page gets a search
field that shows the parsed qualifiers as removable chips (`is:open author:priya label:bug sort:updated`), filter menus
(Author, Label, Milestone, Assignee, plus Review on PRs, and Sort) with a search box and checkmarks inside each, a
"Clear filters" action, and two empty states. Rows reuse `components.IssueListRow` and `components.PRListRow` as they
are, now fed priority and comment counts. PR rows add a review-state indicator shaped like the CI badge. On mobile, the
menus collapse behind one "Filters" button.

Open `mock.html` over HTTP from the repo root, at `/.scratch/2026-11-06-issue-pr-list-filters/mock.html`. Rebuild the
CSS with `bin/tailwindcss -i .scratch/2026-11-06-issue-pr-list-filters/mock-input.css -o .scratch/2026-11-06-issue-pr-list-filters/mock.css`.
The hash sets the state, e.g. `#list=pulls&preset=sample&menu=label`, `#list=issues&data=zero` or `&filters=open`.
Screenshots go in `shots/` (gitignored, not committed). `mock.html` is generated, so its rows are
repetitive by design.

## States

- Issues and PRs, each with a default page (pinned issues and "Page 1 of N"), filtered results (chips, the trigger
  values, "N results · Clear filters"), an open menu (multi-select labels with tick boxes, single-select menus with a
  check), no matches (`EmptyStateWithHint` plus a Clear filters button) and a repo with no items (empty state plus a
  New issue / New pull request button, with the toolbar hidden).
- Mobile: the field and the "Filters (n)" button share a row. The menus stack full width below the button; each
  popover spans the column.
- Interactive: menus filter the rows. Typing `label:bug author:@daisy` and pressing Enter parses it into chips,
  Backspace in an empty field drops the last chip, and the tabs set `is:`.

## What backs each control

| Control                         | Backed by                                                                        | Missing                                                                                                                                                                                                                       |
| ------------------------------- | -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Issues: free text               | `PageIssues` `q` (title substring)                                               | Qualifiers in `q` aren't parsed, yet the page advertises `label:`, `milestone:` and `author:`. A small parser for `is: author: label: milestone: assignee: no: sort:` is needed, shared by both lists.                        |
| Issues: Label / Milestone / Sort | `label=` (one name), `milestone=` (ID), `sort=newest/oldest/recently-updated`   | Multiple labels (AND), `no:label` / `no:milestone`, a milestone by title (`milestone:v0.3`), and `sort=comments`.                                                                                                              |
| Issues: Author / Assignee       | —                                                                                | No filter. `AssigneeService` exists, but `PageIssues` doesn't batch-load assignees. `@me` needs the caller ID.                                                                                                                |
| Issue row priority + comments   | `model.Issue.Priority`; account pages count comments (`account_page_handler.go`) | `PageIssues` doesn't pass `Priority` / `Comments` to `IssueListRow`, so it needs a per-repo comment-count batch.                                                                                                             |
| PR list: any search or filter   | —                                                                                | `PagePulls` reads only `state`. Everything else (q, author, label, milestone, assignee, review, sort) is new. `service.PullListRow` already carries labels, reviewers and the comment count.                               |
| PR review state                 | `PullListRow.Reviewers` (`[]PullReview` with `State`)                            | A `ReviewState` field on `PRListRowData` and a rule for collapsing it (any changes requested → that; approvals ≥ required → approved; else review required). "Required" needs the protection rule per base branch. |
| Pagination                      | `components.PaginationWithCount`; `Pull.ListWithCIStatus(…, limit, offset)`      | `PagePulls` passes `0, 0`. Issues filter in Go after `Issue.List` loads every issue, so paging needs filtering pushed into SQL first (or paging after the in-memory filter as a stopgap). Tab counts must stay unfiltered. |
| Clear filters / chips           | —                                                                                | Server-rendered from the parsed query. Each chip's × is a link to the same URL without that qualifier.                                                                                                                       |

## Open questions

- Should the menus write into `q` (one source of truth, GitHub-style) or keep separate `label=` / `milestone=` params?
  The mock assumes `q`, with the old params kept as aliases.
- Is `assignee:@me` worth it before there's a "mine" tab?
- Should page size be fixed (25 in the mock) or configurable, and offset or cursor? `PaginationWithCount` implies we
  know the total, which the tab counts already give.
- Should pinned issues hide while filters are active? The mock hides them.
- On mobile the four PR tabs plus large counts brush the right edge at 390 px (true of today's page as well).
- The shell header overflows to about 490 px at 390 px wide in every mock (`mockups/shell.js`), so mobile screenshots
  are clipped to the viewport.
