# Mock coverage

Which real pages (`internal/view/pages/*.templ`) have a mock, and which do not. Checked against the router and templates
in November 2026; update it when a mock is added.

## Pages with no mock

| Real page               | Route                                    | Notes           |
| ----------------------- | ---------------------------------------- | --------------- |
| `setup.templ`           | `/setup`                                 | First-run setup |
| `oauth_authorize.templ` | `/oauth/authorize`                       |                 |
| `device_login.templ`    | `/login/device`, `/login/device/confirm` |                 |
| `not_found.templ`       | any 404                                  |                 |

## Pages covered by a differently named mock

| Mock                                      | Real page                                         |
| ----------------------------------------- | ------------------------------------------------- |
| `actions.html`                            | `checks.templ`: the repo Checks page              |
| `pr.html`                                 | `pull_detail.templ`                               |
| `project.html`, `project_board.html`      | `projects.templ`, `project_detail.templ`          |
| `profile.html`, `org_profile.html`        | `user.templ`, `org.templ`                         |
| `settings.html`, `account_settings.html`  | `repo_settings.templ`, `settings.templ`           |
| `new_repo.html`                           | `repo_new.templ`                                  |
| `my_repositories.html`, `my_pulls.html`   | `account_repos.templ`, `account_pulls.templ`      |
| `my_issues.html`, `attention.html`        | `account_issues.templ`, `account_attention.templ` |
| `wiki*.html` (six pages)                  | `wiki_page.templ`, `wiki_shared.templ`            |
| `discussion_detail_locked.html`           | The locked state of `discussion_detail.templ`     |
| `issues_closed.html`, `*_closed/answered` | Tab states of the list pages                      |
| `issues_filtered.html`                    | `issues.templ` with filter chips and a menu open  |

## States with no mock

Mocks show the happy path. These real states are not drawn:

- **Empty states.** Issue and PR lists with no match ("No open issues match these filters."); repo with no commits
  ("This repository is empty."), empty directory, no releases, no milestones, no projects, no wiki page ("This wiki has
  no Home page yet."), empty discussion and PR lists, no checks (the curl example).
- **Unrecognized qualifier notice.** A list search holding a token such as `sort:comments-desc` shows a line under the
  search field saying it was searched as text (`ListFilterBar`'s `Notices`).
- **Read-only and signed-out views.** No Edit, Delete or Manage controls; "Sign in to reply."; a locked issue the viewer
  cannot comment on; the `pr.html` merge box's lock line ("Only collaborators with write access…") in place of Merge and
  Close.
- **PR states.** `pr.html` opens in other states through hashes: `#pr=open|draft|merged`,
  `#merge=ready|needs|checks-pending|failing|changes|conflicts|auto`, `#viewer=reviewer|author|readonly`, `#manage=yes`
  (a repo admin or owner, who gets Dismiss review on a blocking request for changes; join with `&`). The `conflicts` box
  carries the command-line steps, `ready` offers Update branch, and `viewer=readonly` on `ready` reads "Ready, waiting
  for a maintainer". A dismissed review's timeline entry ("Dismissed by …: reason") has no mock.
  `pr_files.html#merged=unavailable` is a merged PR whose diff was never recorded ("Diff unavailable after merge."); the
  Commits tab says the same for its commits and has no mock.
- **Other states.** Closed pull requests; archived and mirror repo banners; binary file in `blob`; hidden-issue project
  card; form validation errors (`Alert` destructive) on every create page but `new_file.html` (submit a blank, `.git` or
  slash-ended path, or open `#error`); inline field errors on `login.html`, `register.html`, `new_repo.html` and
  `issue_new.html` (submit a blank field, or open `?error=1`); one-time token and backup-code reveals.
- **Requests in flight and failed.** Mocks send no requests, so no button shows the busy spinner (`aria-busy`, from
  `static/htmx-busy.js`) and no failed save shows its error toast. The styles are shared: setting `aria-busy="true"` on
  a mock button draws the real busy state.
- **Filters and navigation.** `?category=` on discussions, the command palette, organization people and repository tabs,
  and the profile README call to action.
- **Discovery.** `forgot_password.html?nosmtp=1` draws the reset page of an instance that can't send email, with the
  administrators it names. Explore with no match ("No public repositories match your filters."), and the default branch
  of a repository with no commits, which stays a text box because the first push creates the branch, have no mock.
- **Other workflows.** Pull-mirror settings, a pending transfer, home load warnings, the issue template picker, and PR
  diff error states.
- **Admin mode.** `admin_mode.html` draws the page in both states (add `?admin_mode=1` for the running one), and any
  mock with `?admin_mode=1` or `data-admin-mode` on `#cz-header` shows the "Acting as instance admin" banner and drops
  "Enter admin mode" from the user menu. `forbidden.html` draws the 403 page, with the superadmin hint behind
  `?admin_hint=1`. The identity dialog that "Enter admin mode" opens is drawn with its password factor only (type
  `wrong` for the inline refusal, or open `?identity_error=1`).
- **Admin.** The identity-confirm dialog (password, two-factor and provider factors), the SSO success and error banners,
  and users and audit-log pages beyond the first. The mock uses the generic confirm dialog instead.

## Differences found in the real app

Seen while porting, not fixed:

- A closed discussion has no UI: no banner, no Reopen, no "closed this" event. Only the locked state exists.

## Controls that are mocked or not wired

Server-side actions are faked in the browser: removing a collaborator, label, rule, webhook, deploy key or org member,
revoking a key, webhook deliveries, archive and unarchive, changing or deleting a repository (typed-name dialogs that
accept any password), leaving an org, deleting a comment, accepting or declining a transfer, create and delete branch or
tag, and Watch and Star toggles. They use the real confirm dialogs, toasts and empty states.

Some controls and forms have no behaviour. In particular:

- **Add forms and saves** in `settings.html`, `org_settings.html`, and `account_settings.html` (Add key, label, rule and
  webhook, Save changes, member Role selects, and Save profile): these do not persist changes; several forms suppress
  submission without feedback.
- **File deletion** in `blob.html`: confirmation shows feedback but leaves the file displayed.
- **Mock-only state:** `account_settings.html?smtp=off` shows the Email section with no outgoing email (no Add email
  button, the note instead).
- **Disabled in the real app too:** the Teams panel in `org_settings.html`.
- **Links to a page with no mock:** `org_profile.html` "Repositories" tab, and any link to a page in the table above.
- **Auth states:** the sign-in error banner, the email-first signup flow, expired and invalid reset or verify links, and
  the Sign-in page with Forgot password? absent (it is always shown now).

## Deliberate differences from the real app

- **Project board cards.** Every card opens the card dialog, with the linked issue or PR shown as a link inside it. In
  the real app only note cards open the dialog; a linked issue or PR card navigates straight to that item.
- **PR checks page.** The repo tab bar highlights Checks. The real app keeps Pull requests highlighted on every PR tab.
- **Repository settings webhooks.** `settings.html` always shows "An organization webhook also fires for this
  repository"; the real page renders it only when the owning organization has a webhook, and never for a personal repo.
