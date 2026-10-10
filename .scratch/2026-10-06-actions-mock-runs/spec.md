# Actions tab shows mockup CI runs on every repository

Created: 2026-10-06
Category: bug
Status: done

## Problem

Every repository has an Actions tab. `internal/view/fragments/repo_subnav.templ` renders it unconditionally, and no repo setting hides it. It leads to `GET /{owner}/{repo}/actions` (`internal/router/router.go`, `PageActions` in `internal/handler/actions_page_handler.go`). The page is the placeholder that UI overhaul phase 4 added. It shows an "Actions are coming soon." empty state, followed by two run rows carried over from the mockup (`internal/view/pages/actions.templ`):

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
- `internal/view/pages/issues.templ` (issues list): "Filter syntax: `is:open`, `label:bug`, `milestone:v0.3`, `author:daisy`", and the search box placeholder "Filter is:open". `PageIssues` (`internal/handler/issue_page_handler.go`) only matches a case-insensitive substring of the title, so none of these qualifiers work. "daisy" and "v0.3" come from the mockup.
- `internal/view/components/comment_editor.templ` (comment box on issues, PRs and discussions): "Markdown supported · drop files to attach". Comments have no drop or upload handling; the only drop handlers are on the project board and the wiki.
- `internal/view/pages/pull_new.templ` (new PR): "Suggested from the files changed." under a picker built from `data.Reviewer.All`. The handler computes `Reviewer.Suggested` with `PullService.SuggestReviewers`, but this page never renders it.
- `internal/view/pages/repo_new.templ` (new repository): "Need inspiration? `turbo-octo-meme`". It's always the same name, copied from GitHub's random suggestion.
- `internal/view/pages/new_organization.templ` (new organization): "My personal account / An enterprise account" and "Free / Team" radios. They have no `name`, so nothing is submitted. Enterprise and Team are disabled and labelled as unavailable.

## Acceptance criteria

- [x] No page renders hardcoded CI runs. `actions.templ`, `PageActions` and `ActionsData` are gone, and `GET /{owner}/{repo}/actions` returns 404.
- [x] Every repo's subnav has a "Checks" tab linking to `/{owner}/{repo}/checks`, active on that page.
- [x] `/checks` shows one card per commit that has reported statuses, ordered by the commit's most recent status update. Each card shows:
  - the combined state (`error` > `failure` > `pending` > `success`, as the status API combines them)
  - the commit subject, or the bare SHA when the commit isn't in the repository (the status API doesn't check SHAs)
  - "N of M passed", the short SHA linking to `/{owner}/{repo}/commit/{sha}`, and the relative time
  - each context, with its state icon, its description, and a Details link when `target_url` is set
- [x] Commits paginate with Newer/Older links, the way the commit history does.
- [x] `/checks` follows the same read access as the other repo pages: a private repo is a 404 to anyone who can't read it.
- [x] With no statuses, the page explains how to post one: a `curl` example built from `server.base_url` and the repo's owner and name.
- [x] The PR Checks tab's link reads "View all checks →" and goes to `/checks`.
- [x] Integration tests cover the store query (grouping, ordering, pagination, repo isolation) and the page (cards, empty state, access).

## Relevant files

- `internal/view/pages/actions.templ`: placeholder page with the mockup rows
- `internal/handler/actions_page_handler.go`: `PageActions`
- `internal/view/viewmodels_social.go`: `ActionsData`
- `internal/view/fragments/repo_subnav.templ`: the tab
- `internal/router/router.go`: `GET /{owner}/{repo}/actions`
- `internal/view/pages/pr_checks.templ`: "View all workflow runs →" link
- `internal/handler/repo_page_access_test.go`: `/actions` in the private-repo page list
- `internal/store/commit_status_store.go`, `internal/service/commit_status_service.go`, `internal/handler/commit_status_handler.go`: Commit Status API
- `internal/db/migrations/021_create_commit_statuses.sql`: `commit_statuses` table
- `docs/ROADMAP.md`: Phase 18

## Comments

**Malith Kuruppu, 2026-10-06.** Triage decisions:

- Option 2: make the page real from commit statuses.
- Name it "Checks", at `/{owner}/{repo}/checks`, matching the PR's Checks tab. Remove `/actions` without a redirect, leaving it free for Phase 18's pipeline runs.
- Layout B: one card per commit (combined state, commit subject, "N of M passed", short SHA, time), with that commit's checks nested under it. The flat one-row-per-status list was rejected.

**Claude, 2026-10-06.** Notes for implementation:

- Don't look up the subject with `CodeService.GetCommit`: it diffs the whole commit. `ResolveRef` is cheap, but it tries branch names before SHAs, so a status posted against `main` would show the branch tip's subject. Parse the stored SHA as a full hash and read the commit object directly. If that fails, render the SHA alone.
- Today the only index is `(repo_id, sha)`. Page the SHAs with `GROUP BY sha ORDER BY MAX(updated_at) DESC, sha` and `LIMIT pageSize+1` for `HasMore`, then load the page's statuses with one `IN (...)` query (numbered `$N` params, per CLAUDE.md). Add an index migration only if `EXPLAIN` shows the existing index doesn't serve the repo filter.
- Move the worst-case combine in `CommitStatusStore.GetCombined` into a helper the page shares, so the two can't drift.
- `internal/view/pages/time_helpers.go` has `relativeTime`. `components.PaginationLabeled` gives Newer/Older links.
- `internal/handler/repo_page_access_test.go` lists `/actions` among the private-repo pages; change it to `/checks`. ROADMAP Phase 18.2/18.3 still say `/actions` will list pipeline runs, which stays true.

**Claude, 2026-10-06.** Done. `EXPLAIN` showed `idx_commit_statuses_repo_sha` serving the repo filter, and an `(repo_id, updated_at)` index wouldn't remove the sort over `MAX(updated_at)`, so there's no migration. A page past the end shows "No more checks." with a Newer link instead of the empty state.
