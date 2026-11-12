# Mock coverage

Which real pages (`internal/view/pages/*.templ`) have a mock, and which do not. Checked against the router and templates
in November 2026; update it when a mock is added.

## Generated pages

Rendered from the real templates by `cmd/mockgen` into `mockups/generated/`; see the [README](./README.md). A page that
cannot render from a plain view-model is listed under "Pages with no mock" with the reason.

<!-- mockgen:start -->

| Page           | Variant         | Shows                                                                | Template             |
| -------------- | --------------- | -------------------------------------------------------------------- | -------------------- |
| `home`         | `default`       | signed in with repositories                                          | `home.templ`         |
| `home`         | `anon`          | signed out                                                           | `home.templ`         |
| `home`         | `empty`         | no repositories yet                                                  | `home.templ`         |
| `home`         | `admin-mode`    | instance admin acting with elevated rights                           | `home.templ`         |
| `login`        | `default`       | email and password; the instance cannot send email, so no reset link | `login.templ`        |
| `login`        | `reset-link`    | the instance can send email                                          | `login.templ`        |
| `login`        | `error`         | wrong password                                                       | `login.templ`        |
| `login`        | `sso`           | LDAP and SAML enabled                                                | `login.templ`        |
| `not_found`    | `default`       | signed in                                                            | `not_found.templ`    |
| `not_found`    | `anon`          | signed out                                                           | `not_found.templ`    |
| `repo`         | `default`       | writer on a repository with a README                                 | `repo.templ`         |
| `repo`         | `viewer`        | signed in without write access: no Add file, no edit                 | `repo.templ`         |
| `repo`         | `anon`          | signed out: Star and Fork lead to sign-in                            | `repo.templ`         |
| `repo`         | `empty`         | no commits yet                                                       | `repo.templ`         |
| `repo`         | `empty-viewer`  | no commits yet, without write access                                 | `repo.templ`         |
| `repo`         | `archived`      | archived banner, read-only                                           | `repo.templ`         |
| `repo`         | `mirror`        | pull mirror with a sync banner                                       | `repo.templ`         |
| `repo`         | `mirror-failed` | pull mirror whose last sync failed                                   | `repo.templ`         |
| `repo`         | `fork`          | a fork shows its source                                              | `repo.templ`         |
| `tree`         | `default`       | a directory with the file tree open                                  | `tree.templ`         |
| `tree`         | `empty`         | an empty directory                                                   | `tree.templ`         |
| `blob`         | `default`       | writer: Edit and Delete                                              | `blob.templ`         |
| `blob`         | `viewer`        | read-only: no Edit or Delete                                         | `blob.templ`         |
| `blob`         | `binary`        | a binary file offers only the raw download                           | `blob.templ`         |
| `blame`        | `default`       | per-line history with runs of one commit                             | `blame.templ`        |
| `edit_file`    | `default`       | editing an existing file                                             | `edit_file.templ`    |
| `edit_file`    | `conflict`      | the file changed after the page loaded                               | `edit_file.templ`    |
| `new_file`     | `default`       | creating a file in the repository root                               | `new_file.templ`     |
| `new_file`     | `error`         | a refused path with the upload to choose again                       | `new_file.templ`     |
| `commits`      | `default`       | a middle page, with newer and older links                            | `commits.templ`      |
| `commits`      | `empty`         | no commits on this ref                                               | `commits.templ`      |
| `commit`       | `default`       | a commit with checks, an edit, a new file and a binary               | `commit.templ`       |
| `commit`       | `no-checks`     | no commit statuses                                                   | `commit.templ`       |
| `refs`         | `default`       | writer: create and delete branches and tags                          | `refs.templ`         |
| `refs`         | `viewer`        | read-only lists                                                      | `refs.templ`         |
| `refs`         | `empty`         | no branches or tags                                                  | `refs.templ`         |
| `contributors` | `default`       | commit counts with a weekly sparkline                                | `contributors.templ` |
| `contributors` | `empty`         | no commits yet                                                       | `contributors.templ` |
| `pulse`        | `default`       | 30 days of issues, pull requests and commits                         | `pulse.templ`        |
| `pulse`        | `empty`         | no activity                                                          | `pulse.templ`        |
| `dependencies` | `default`       | Go and npm manifests                                                 | `dependencies.templ` |
| `dependencies` | `empty`         | no manifest found                                                    | `dependencies.templ` |
| `stargazers`   | `default`       | people who starred                                                   | `stargazers.templ`   |
| `stargazers`   | `empty`         | no stars yet                                                         | `stargazers.templ`   |
| `activity`     | `default`       | every kind of event, more pages to come                              | `activity.templ`     |
| `activity`     | `empty`         | no activity in this scope                                            | `activity.templ`     |
| `activity`     | `error`         | the feed query failed                                                | `activity.templ`     |
| `code_search`  | `default`       | results with the match highlighted                                   | `code_search.templ`  |
| `code_search`  | `empty`         | no results                                                           | `code_search.templ`  |
| `code_search`  | `no-query`      | before a search                                                      | `code_search.templ`  |
| `search`       | `default`       | results of every kind                                                | `search.templ`       |
| `search`       | `repos`         | repositories tab only                                                | `search.templ`       |
| `search`       | `empty`         | no results                                                           | `search.templ`       |
| `search`       | `no-query`      | before a search                                                      | `search.templ`       |
| `search`       | `anon`          | signed out                                                           | `search.templ`       |
| `topic`        | `default`       | repositories under a topic, sorted by stars                          | `topic.templ`        |
| `topic`        | `empty`         | no repositories                                                      | `topic.templ`        |
| `repo_fork`    | `default`       | choose where to fork                                                 | `repo_fork.templ`    |
| `repo_fork`    | `existing`      | the viewer already has a fork                                        | `repo_fork.templ`    |
| `repo_fork`    | `no-owner`      | no namespace to fork into                                            | `repo_fork.templ`    |

<!-- mockgen:end -->

## Pages with no mock

| Real page               | Route                                    | Notes           |
| ----------------------- | ---------------------------------------- | --------------- |
| `explore.templ`         | `/explore`                               |                 |
| `setup.templ`           | `/setup`                                 | First-run setup |
| `oauth_authorize.templ` | `/oauth/authorize`                       |                 |
| `device_login.templ`    | `/login/device`, `/login/device/confirm` |                 |

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

- **Empty states.** Issue and PR lists with no match ("No open issues match these filters."); no releases, no
  milestones, no projects, no wiki page ("This page does not exist yet."), empty discussion and PR lists, no checks (the
  curl example). The repository, directory, commit, search and stargazer empty states are generated `--empty` variants.
- **Unrecognized qualifier notice.** A list search holding a token such as `sort:comments-desc` shows a line under the
  search field saying it was searched as text (`ListFilterBar`'s `Notices`).
- **Read-only and signed-out views.** "Sign in to reply."; a locked issue the viewer cannot comment on; the `pr.html`
  merge box's lock line ("Only collaborators with write access…") in place of Merge and Close.
- **PR states.** `pr.html` opens in other states through hashes: `#pr=open|draft|merged`,
  `#merge=ready|needs|checks-pending|failing|changes|conflicts|auto`, `#viewer=reviewer|author|readonly` (join with
  `&`).
- **Other states.** Closed pull requests; hidden-issue project card; form validation errors (`Alert` destructive) on
  every create page but `new_file.html` (submit a blank, `.git` or slash-ended path, or open `#error`); one-time token
  and backup-code reveals.
- **Repository heatmap.** The overview's commit heatmap is positioned from `time.Now`, so a generated mock would change
  daily; the fixture leaves it out and the section does not render.
- **Raw file.** `raw.html` stays hand-written: `/raw/...` serves plain text, not a templ page.
- **Requests in flight and failed.** Mocks send no requests, so no button shows the busy spinner (`aria-busy`, from
  `static/htmx-busy.js`) and no failed save shows its error toast. The styles are shared: setting `aria-busy="true"` on
  a mock button draws the real busy state.
- **Filters and navigation.** `?category=` on discussions, the command palette, organization people and repository tabs,
  and the profile README call to action.
- **Other workflows.** Pull-mirror settings, a pending transfer, home load warnings, the issue template picker, and PR
  diff error states.
- **Admin mode.** `admin_mode.html` draws the page in both states (add `?admin_mode=1` for the running one), and any
  mock with `?admin_mode=1` or `data-admin-mode` on `#cz-header` shows the "Acting as instance admin" banner and drops
  "Enter admin mode" from the user menu. `forbidden.html` draws the 403 page, with the superadmin hint behind
  `?admin_hint=1`. Not drawn: the identity dialog that "Enter admin mode" opens.
- **Admin.** The identity-confirm dialog (password, two-factor and provider factors), the SSO success and error banners,
  and users and audit-log pages beyond the first. The mock uses the generic confirm dialog instead.

## Differences found in the real app

Seen while porting, not fixed:

- A closed discussion has no UI: no banner, no Reopen, no "closed this" event. Only the locked state exists.

## Controls that are mocked or not wired

Server-side actions are faked in the browser: removing a collaborator, label, rule, webhook, deploy key or org member,
revoking a key, webhook deliveries, archive and unarchive, leaving an org, deleting a comment, accepting or declining a
transfer, create and delete branch or tag, and Watch and Star toggles. They use the real confirm dialogs, toasts and
empty states.

Some controls and forms have no behaviour. In particular:

- **Add forms and saves** in `settings.html`, `org_settings.html`, and `account_settings.html` (Add key, label, rule and
  webhook, Save changes, member Role selects, and Save profile): these do not persist changes; several forms suppress
  submission without feedback.
- **Disabled in the real app too:** the Teams and org webhooks panels in `org_settings.html`.
- **Links to a page with no mock:** `org_profile.html` "Repositories" tab, and any link to a page in the table above.
- **Auth states:** the sign-in error banner, the email-first signup flow, expired and invalid reset or verify links, and
  the "instance can't send email" state.

## Deliberate differences from the real app

- **Project board cards.** Every card opens the card dialog, with the linked issue or PR shown as a link inside it. In
  the real app only note cards open the dialog; a linked issue or PR card navigates straight to that item.
- **PR checks page.** The repo tab bar highlights Checks. The real app keeps Pull requests highlighted on every PR tab.
