# Actions tab shows mockup CI runs on every repository

Created: 2026-10-06
Category: bug
Status: needs-triage

## Problem

Every repository has an Actions tab. `internal/view/fragments/repo_subnav.templ` renders it unconditionally, and no repo setting hides it. It leads to `GET /{owner}/{repo}/actions` (`internal/router/router.go`, `PageActions` in `internal/handler/page_actions_handler.go`). The page is the placeholder that UI overhaul phase 4 added (`docs/superpowers/plans/2026-05-14-ui-overhaul-phase-4-tracker.md`). It shows an "Actions are coming soon." empty state, followed by two run rows carried over from the mockup (`internal/view/pages/actions.templ`):

- `CI · Go test` / `build · main · 2m 14s` / `just now`
- `CI · Go test` / `build · main · 1m 58s` / `2 days ago`

The rows are dimmed (`opacity-40`), `pointer-events-none` and `aria-hidden`, but sighted users still read them as runs of this repository. They name a `main` branch the repo may not have, durations nothing measured, and a "just now" that never changes. `PageActions` passes no run data, and none exists: no CI runner has been built (ROADMAP Phase 18.1–18.3, all planned).

There's a second way in. On a PR's Checks tab (`/{owner}/{repo}/pulls/{number}/checks`), once any status is reported, `internal/view/pages/pr_checks.templ` links "View all workflow runs →" to `/actions`. A user who has just looked at real checks follows that link to "coming soon" and the fake runs.

What does exist is the Commit Status API (Phase 3.2), which external CI uses to report results:

- `POST /api/repos/{owner}/{repo}/statuses/{sha}` (write access) upserts one status per `(repo, sha, context)`
- `GET /api/repos/{owner}/{repo}/statuses/{sha}` and `GET /api/repos/{owner}/{repo}/commits/{sha}/status` read them back

Reported statuses show on the commit page (`/{owner}/{repo}/commit/{sha}`) and on the PR Checks tab. Branch protection's required checks read them too, and #165 made a merge use the head commit those checks passed on. Nothing lists them across the repository.

## Options

1. **Remove the tab until Phase 18.** Delete the subnav tab, the route, `PageActions`, `ActionsData` and `actions.templ`, and drop the PR Checks link. The route then 404s. Phase 18.3 adds the page back with real pipeline runs.
2. **Make the page real from commit statuses.** List the statuses external CI has posted, newest first, linking each commit. When there are none, the empty state explains how to post one through the status API. "Actions" suggests a built-in runner, so this likely wants another name (for example "Checks" or "CI"). Phase 18's runner is planned to report through the same status API, so the page stays useful once it lands. A repo-wide newest-first query needs a store method and probably an index on `(repo_id, updated_at)`, because the only index today is `(repo_id, sha)`.
3. **Something else.**

Settings → Access has toggles for Issues, Discussions, Projects and Wiki (`allow_*` columns from migration 058), but none for Actions. Phase 18.2 plans a "CI/CD" settings section to enable pipelines. That fits a tab with something behind it, not a placeholder.

## Other mockup leftovers

A sweep of every `.templ` file under `internal/view/` (2026-10-06) found no other dimmed mockup rows. It did find visible mockup copy that claims behaviour the code doesn't have. These are out of scope for this ticket. Each wants its own:

- `internal/view/layout/layout.templ` (footer of every page): `● all systems normal` in a `role="status"` span. It's a literal, and nothing checks any system.
- `internal/view/pages/issues.templ` (issues list): "Filter syntax: `is:open`, `label:bug`, `milestone:v0.3`, `author:daisy`", and the search box placeholder "Filter is:open". `PageIssues` (`internal/handler/page_issue_handler.go`) only matches a case-insensitive substring of the title, so none of these qualifiers work. "daisy" and "v0.3" come from the mockup.
- `internal/view/components/comment_editor.templ` (comment box on issues, PRs and discussions): "Markdown supported · drop files to attach". Comments have no drop or upload handling; the only drop handlers are on the project board and the wiki.
- `internal/view/pages/pull_new.templ` (new PR): "Suggested from the files changed." under a picker built from `data.Reviewer.All`. The handler computes `Reviewer.Suggested` with `PullService.SuggestReviewers`, but this page never renders it.
- `internal/view/pages/repo_new.templ` (new repository): "Need inspiration? `turbo-octo-meme`". It's always the same name, copied from GitHub's random suggestion.
- `internal/view/pages/new_organization.templ` (new organization): "My personal account / An enterprise account" and "Free / Team" radios. They have no `name`, so nothing is submitted. Enterprise and Team are disabled and labelled as unavailable.

## Acceptance criteria

To be set once an option is chosen.

## Relevant files

- `internal/view/pages/actions.templ`: placeholder page with the mockup rows
- `internal/handler/page_actions_handler.go`: `PageActions`
- `internal/view/viewmodels_social.go`: `ActionsData`
- `internal/view/fragments/repo_subnav.templ`: the tab
- `internal/router/router.go`: `GET /{owner}/{repo}/actions`
- `internal/view/pages/pr_checks.templ`: "View all workflow runs →" link
- `internal/handler/repo_page_access_test.go`: `/actions` in the private-repo page list
- `internal/store/commit_status_store.go`, `internal/service/commit_status_service.go`, `internal/handler/commit_status_handler.go`: Commit Status API
- `internal/db/migrations/021_create_commit_statuses.sql`: `commit_statuses` table
- `docs/ROADMAP.md`: Phase 18
