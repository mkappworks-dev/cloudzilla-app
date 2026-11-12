# Mock coverage

Which real pages (`internal/view/pages/*.templ`) have a mock, and which do not. Checked against the router and templates
in November 2026; update it when a mock is added.

## Generated pages

Rendered from the real templates by `cmd/mockgen` into `mockups/generated/`; see the [README](./README.md). A page that
cannot render from a plain view-model is listed under "Pages with no mock" with the reason.

<!-- mockgen:start -->

| Page             | Variant          | Shows                                                                   | Template               |
| ---------------- | ---------------- | ----------------------------------------------------------------------- | ---------------------- |
| `home`           | `default`        | signed in with repositories                                             | `home.templ`           |
| `home`           | `anon`           | signed out                                                              | `home.templ`           |
| `home`           | `empty`          | no repositories yet                                                     | `home.templ`           |
| `home`           | `admin-mode`     | instance admin acting with elevated rights                              | `home.templ`           |
| `login`          | `default`        | email and password; the instance cannot send email, so no reset link    | `login.templ`          |
| `login`          | `reset-link`     | the instance can send email                                             | `login.templ`          |
| `login`          | `error`          | wrong password                                                          | `login.templ`          |
| `login`          | `sso`            | LDAP and SAML enabled                                                   | `login.templ`          |
| `not_found`      | `default`        | signed in                                                               | `not_found.templ`      |
| `not_found`      | `anon`           | signed out                                                              | `not_found.templ`      |
| `settings`       | `default`        | repository owner                                                        | `repo_settings.templ`  |
| `settings`       | `manager`        | can manage but does not own: no transfer, delete or visibility controls | `repo_settings.templ`  |
| `settings`       | `archived`       | archived repository banner and Unarchive                                | `repo_settings.templ`  |
| `settings`       | `mirror`         | pull mirror: remote, interval, token, last sync                         | `repo_settings.templ`  |
| `settings`       | `mirror-failing` | pull mirror whose last sync failed                                      | `repo_settings.templ`  |
| `settings`       | `transfer`       | ownership transfer awaiting its recipient                               | `repo_settings.templ`  |
| `wiki`           | `default`        | repository owner: Edit, Delete, New page and page reordering            | `wiki_page.templ`      |
| `wiki`           | `writer`         | write access without manage: Edit and New page, no Delete or reordering | `wiki_page.templ`      |
| `wiki`           | `viewer`         | read-only                                                               | `wiki_page.templ`      |
| `wiki`           | `empty`          | page does not exist yet                                                 | `wiki_page.templ`      |
| `wiki_edit`      | `default`        | editing Home                                                            | `wiki_edit.templ`      |
| `wiki_new`       | `default`        | blank form                                                              | `wiki_new.templ`       |
| `project`        | `default`        | open boards, writer                                                     | `projects.templ`       |
| `project`        | `viewer`         | signed in, read-only                                                    | `projects.templ`       |
| `project`        | `anon`           | signed out                                                              | `projects.templ`       |
| `project`        | `empty`          | no boards yet                                                           | `projects.templ`       |
| `project_board`  | `default`        | writer: drag, edit and add                                              | `project_detail.templ` |
| `project_board`  | `viewer`         | read-only                                                               | `project_detail.templ` |
| `releases`       | `default`        | writer: draft, latest, prerelease and plain releases                    | `releases.templ`       |
| `releases`       | `viewer`         | read-only: drafts are hidden by the server, so none here                | `releases.templ`       |
| `releases`       | `empty`          | no releases yet                                                         | `releases.templ`       |
| `release_detail` | `default`        | latest release, writer                                                  | `release_detail.templ` |
| `release_detail` | `viewer`         | read-only                                                               | `release_detail.templ` |
| `release_detail` | `prerelease`     | marked as a prerelease                                                  | `release_detail.templ` |
| `release_detail` | `draft`          | unpublished draft with Publish                                          | `release_detail.templ` |
| `release_new`    | `default`        | blank form                                                              | `release_new.templ`    |
| `gists`          | `default`        | public gists, signed in                                                 | `gists.templ`          |
| `gists`          | `private`        | your private gists                                                      | `gists.templ`          |
| `gists`          | `anon`           | signed out: no private tab                                              | `gists.templ`          |
| `gists`          | `empty`          | no public gists                                                         | `gists.templ`          |
| `gist_detail`    | `default`        | owner: Edit and Delete                                                  | `gist_detail.templ`    |
| `gist_detail`    | `viewer`         | someone else's gist                                                     | `gist_detail.templ`    |
| `gist_edit`      | `default`        | two files                                                               | `gist_edit.templ`      |
| `gist_new`       | `default`        | blank form                                                              | `gist_new.templ`       |

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
| `profile.html`, `org_profile.html`        | `user.templ`, `org.templ`                         |
| `account_settings.html`                   | `settings.templ`                                  |
| `new_repo.html`                           | `repo_new.templ`                                  |
| `my_repositories.html`, `my_pulls.html`   | `account_repos.templ`, `account_pulls.templ`      |
| `my_issues.html`, `attention.html`        | `account_issues.templ`, `account_attention.templ` |
| `discussion_detail_locked.html`           | The locked state of `discussion_detail.templ`     |
| `issues_closed.html`, `*_closed/answered` | Tab states of the list pages                      |
| `issues_filtered.html`                    | `issues.templ` with filter chips and a menu open  |

## States with no mock

Mocks show the happy path. These real states are not drawn:

- **Empty states.** Issue and PR lists with no match ("No open issues match these filters."); repo with no commits
  ("This repository is empty."), empty directory, no milestones, empty discussion and PR lists, no checks (the curl
  example).
- **Unrecognized qualifier notice.** A list search holding a token such as `sort:comments-desc` shows a line under the
  search field saying it was searched as text (`ListFilterBar`'s `Notices`).
- **Read-only and signed-out views.** No Edit, Delete or Manage controls; "Sign in to reply."; a locked issue the viewer
  cannot comment on; the `pr.html` merge box's lock line ("Only collaborators with write access…") in place of Merge and
  Close.
- **PR states.** `pr.html` opens in other states through hashes: `#pr=open|draft|merged`,
  `#merge=ready|needs|checks-pending|failing|changes|conflicts|auto`, `#viewer=reviewer|author|readonly` (join with
  `&`).
- **Other states.** Closed pull requests; binary file in `blob`; form validation errors (`Alert` destructive) on every
  create page but `new_file.html` (submit a blank, `.git` or slash-ended path, or open `#error`); one-time token and
  backup-code reveals.
- **Requests in flight and failed.** Mocks send no requests, so no button shows the busy spinner (`aria-busy`, from
  `static/htmx-busy.js`) and no failed save shows its error toast. The styles are shared: setting `aria-busy="true"` on
  a mock button draws the real busy state.
- **Filters and navigation.** `?category=` on discussions, the command palette, organization people and repository tabs,
  and the profile README call to action.
- **Other workflows.** Home load warnings, the issue template picker, and PR diff error states.
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

- **Add forms and saves** in `org_settings.html` and `account_settings.html` (Add key, label, rule and webhook, Save
  changes, member Role selects, and Save profile): these do not persist changes; several forms suppress submission
  without feedback.
- **File deletion** in `blob.html`: confirmation shows feedback but leaves the file displayed.
- **Disabled in the real app too:** the Teams and org webhooks panels in `org_settings.html`.
- **Links to a page with no mock:** `org_profile.html` "Repositories" tab, and any link to a page in the table above.
- **Auth states:** the sign-in error banner, the email-first signup flow, expired and invalid reset or verify links, and
  the "instance can't send email" state.

## Deliberate differences from the real app

- **PR checks page.** The repo tab bar highlights Checks. The real app keeps Pull requests highlighted on every PR tab.
