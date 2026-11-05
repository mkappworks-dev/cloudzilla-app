# Mock coverage

Which real pages (`internal/view/pages/*.templ`) have a mock, and which do not. Checked against the router and templates
in November 2026; update it when a mock is added.

## Pages with no mock

| Real page               | Route                                     | Notes           |
| ----------------------- | ----------------------------------------- | --------------- |
| `code_search.templ`     | `/search/code`                            |                 |
| `search.templ`          | `/search`                                 |                 |
| `explore.templ`         | `/explore`                                |                 |
| `admin_settings.templ`  | `/admin/settings`                         |                 |
| `admin_users.templ`     | `/admin/users`, `/admin/users/{username}` |                 |
| `audit_log.templ`       | `/admin/audit-log`                        |                 |
| `sso_settings.templ`    | `/admin/sso`                              |                 |
| `setup.templ`           | `/setup`                                  | First-run setup |
| `oauth_authorize.templ` | `/oauth/authorize`                        |                 |
| `device_login.templ`    | `/login/device`, `/login/device/confirm`  |                 |
| `forbidden.templ`       | any 403                                   |                 |
| `not_found.templ`       | any 404                                   |                 |

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

## States with no mock

Mocks show the happy path. These real states are not drawn:

- **Empty states.** Repo with no commits ("This repository is empty."), empty directory, no releases, no milestones, no
  projects, no wiki page ("This page does not exist yet."), empty discussion and PR lists, no checks (the curl example).
- **Read-only and signed-out views.** No Edit, Delete or Manage controls; "Sign in to reply."; a locked issue the viewer
  cannot comment on.
- **Other states.** Draft, merged and closed pull requests; merge conflicts; failing and pending checks; a draft or
  pre-release release; archived and mirror repo banners; binary file in `blob`; private-issue badge; hidden-issue
  project card; form validation errors (`Alert` destructive) on every create page; one-time token and backup-code
  reveals.
- **Filters.** `?category=` on discussions.

## Differences found in the real app

Seen while porting, not fixed:

- A closed discussion has no UI: no banner, no Reopen, no "closed this" event. Only the locked state exists.

## Controls that are mocked or not wired

Server-side actions are faked in the browser: removing a collaborator, label, rule, webhook, deploy key or org member,
revoking a key, webhook deliveries, archive and unarchive, leaving an org, deleting a comment, accepting or declining a
transfer, create and delete branch or tag, and Watch and Star toggles. They use the real confirm dialogs, toasts and
empty states.

A control with no behaviour shows an "isn't wired up in the mock" toast, and a form with no handler shows "Submitted".
Still in that state:

- **Add forms and saves** in `settings.html` and `org_settings.html` (Add key, label, rule and webhook, Save changes,
  member Role selects): they submit and toast but add no row.
- **Disabled in the real app too:** the Teams and org webhooks panels in `org_settings.html`.
- **Links to a page with no mock:** `org_profile.html` "Repositories" tab, and any link to a page in the table above.
- **Auth states:** the sign-in error banner, the email-first signup flow, expired and invalid reset or verify links, and
  the "instance can't send email" state.
