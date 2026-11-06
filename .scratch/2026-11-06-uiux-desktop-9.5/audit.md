# Mock ↔ templ audit

Created: 2026-11-06 (main @ 1c7e2d2). Desktop scope.

86 mocks in `mockups/` compared against `internal/view/**.templ`. Static code read by 4 auditors + screenshots.

## Verdicts

- 1:1: commits, new_file, dependencies, code_search, topic, discussion_detail, discussion_detail_locked, discussion_new,
  gist_detail, gist_edit, gist_new, activity, stars, organizations, milestones, milestone_new, pr_commits, project,
  admin_settings, sso_settings, login, register, verify_email (~25)
- Minor drift: everything else (~61). Major drift: none, except the account_settings Appearance section (native
  selects vs real SelectMenu with swatches + Chroma preview).

## Systemic drift causes

1. Avatar initials: mocks draw two letters; real `components.Initials` gives one for one-word usernames. Several mocks
   draw avatars as `rounded-sm bg-accent` chips; real Avatar is `rounded-full bg-muted`.
2. Mocks paste base + caller classes side by side; real `withClass` twMerges. In mocks `w-full`/`bg-transparent`/
   `text-sm` win by stylesheet order → wrong widths in admin_users, admin_user, audit_log; totp code input is text-sm
   not text-2xl; invite/signup read-only email loses bg-muted.
3. Page shell gaps: releases*, wiki* mocks lack skip link, `main#main-content`, footer.
4. Links: shell.js "Your stars" → stars.html (real `/{user}?tab=stars`); "All repositories" → my_repositories (real
   `/{owner}?tab=repositories`); subnav count badges lack aria-label; account subnav counts missing.
5. "Details" links in checks mocks have target=_blank; real don't. Code dropdown min-w differs on PR tabs.
6. COVERAGE.md false claims: settings/org_settings/account_settings forms "submit and toast" — they all have
   `onsubmit="return false"` so nothing toasts; draft/pre-release releases and private-issue list badge ARE mocked;
   index.html routes wrong for contributors (/graphs/contributors), dependencies (/network/dependencies),
   my_repositories (/repos); raw.html unlisted.
7. Missing from COVERAGE: pull-mirror settings UI, pending transfer state, ⌘K command palette, org ?tab=people/
   repositories, profile README CTA, home LoadWarnings, issue template picker step, many PR diff error states.

## MOCK-ONLY (mock has it, real doesn't — candidates to build)

- issues list: priority pill + comment count per row (data already exists; account_issues passes both).
- Text-filter "no matches" empty state on /issues, /pulls, /repos, /attention (real leaves an empty bordered box).
- new_organization: live URL slug preview.
- milestone_detail: header/breadcrumb/title update after due-date/title edit (real goes stale).
- pull_new: live "Comparing X → Y" sentence.
- Toasts: fork done, import done, webhook "Redelivery queued" (real 202 with no feedback), admin reset link issued.
- Archived banner on repo settings page.
- line-comment delete button with type=button + aria-label (mock better than real).
- repo.html "N tags" deep link to #tags-list.

## Real-app bugs / UX problems found

- Merge box (merge + Close buttons) renders for any viewer, no CanWrite check; its Close has no confirm.
- No Approve / Request changes UI on PR page; auto-merge + convert-to-draft only in stale fragment; apply-suggestion
  route has no UI.
- /issues never marks private issues; /pulls shows drafts as Open.
- issues.templ advertises label:/milestone:/author: filters the handler doesn't support; placeholder says is:open on
  Closed tab. Repo issue + PR lists have no pagination; PR list has no search/filters.
- pull_new "Suggested from the files changed" shows all reviewers; default head can equal base.
- htmx refresh fragments use old markup: SSH key Delete has NO confirm after a refresh; tokens/admin invitations/refs
  rows restyle after an action.
- Discussion actions reload regardless of status → silent failures. Gist errors use window.alert; gist stars/forks
  always 0; copy promises revision history that doesn't exist.
- Broken a11y names: release_detail aria-labelledby="rel-title" points at nothing; milestone due-date input unlabeled;
  status icons glyph-only; dependency headings h1→h3; custom selects in repo_new lack keyboard handling; wiki reorder
  drag-only; SSO/profile banners lack role; account delete confirm input unlabeled.
- Signed-out repo header hides owner/repo + visibility. No global search below md. Mobile header wraps owner/repo
  names onto two lines; repo subnav tab labels wrap ("Pull / requests"). Notification badge shows "1492" uncapped.
- New file loses typed content on error (bare http.Error). Blob Copy copies line numbers, no feedback. Blame deep-link
  has no highlight. Topic page double container + always-on "Next". Code search shows raw ts_rank and says "public".
- Copy inconsistency: Title Case vs sentence case ("Pull Requests", "Audit Log"), unpluralized counts ("1 accounts",
  "1 reviewers"), lowercase "open" badge on detail vs "Open" in lists.
- Low-contrast label chips for light label colours (e.g. "enhancement" cyan on PR sidebar is nearly invisible).
- Superadmin gets 403 on other users' repo settings and org settings.

## Screenshots

Taken in the audit session's scratchpad and not kept. Re-create them with the harness in [spec.md](./spec.md).
