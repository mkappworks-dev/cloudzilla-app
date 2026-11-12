# Mock coverage

Which real pages (`internal/view/pages/*.templ`) have a mock, and which do not. Checked against the router and templates
in November 2026; update it when a mock is added.

## Generated pages

Rendered from the real templates by `cmd/mockgen` into `mockups/generated/`; see the [README](./README.md). A page that
cannot render from a plain view-model is listed under "Pages with no mock" with the reason.

<!-- mockgen:start -->

| Page                | Variant             | Shows                                                                | Template                  |
| ------------------- | ------------------- | -------------------------------------------------------------------- | ------------------------- |
| `home`              | `default`           | signed in with repositories                                          | `home.templ`              |
| `home`              | `anon`              | signed out                                                           | `home.templ`              |
| `home`              | `empty`             | no repositories yet                                                  | `home.templ`              |
| `home`              | `admin-mode`        | instance admin acting with elevated rights                           | `home.templ`              |
| `login`             | `default`           | email and password; the instance cannot send email, so no reset link | `login.templ`             |
| `login`             | `reset-link`        | the instance can send email                                          | `login.templ`             |
| `login`             | `error`             | wrong password                                                       | `login.templ`             |
| `login`             | `sso`               | LDAP and SAML enabled                                                | `login.templ`             |
| `not_found`         | `default`           | signed in                                                            | `not_found.templ`         |
| `not_found`         | `anon`              | signed out                                                           | `not_found.templ`         |
| `issues`            | `default`           | open issues                                                          | `issues.templ`            |
| `issues`            | `closed`            | the Closed tab                                                       | `issues.templ`            |
| `issues`            | `filtered`          | filter chips, with the Label menu open                               | `issues.templ`            |
| `issues`            | `no-match`          | filters that match nothing                                           | `issues.templ`            |
| `issues`            | `notice`            | an unrecognized qualifier, searched as text, with no match           | `issues.templ`            |
| `issues`            | `empty`             | no issues in any state                                               | `issues.templ`            |
| `issue_detail`      | `default`           | open, for a maintainer                                               | `issue_detail.templ`      |
| `issue_detail`      | `closed`            | closed by a pull request                                             | `issue_detail.templ`      |
| `issue_detail`      | `locked`            | locked, viewed by someone who cannot comment                         | `issue_detail.templ`      |
| `issue_detail`      | `locked-maintainer` | locked, viewed by a maintainer, who can still comment                | `issue_detail.templ`      |
| `issue_detail`      | `viewer`            | signed in without write access: no edit controls                     | `issue_detail.templ`      |
| `issue_detail`      | `anon`              | signed out: no comment box                                           | `issue_detail.templ`      |
| `issue_new`         | `default`           | the form, with the metadata pickers                                  | `issue_new.templ`         |
| `issue_new`         | `chooser`           | the repository has issue templates                                   | `issue_new.templ`         |
| `issue_new`         | `error`             | the title was left blank                                             | `issue_new.templ`         |
| `issue_new`         | `viewer`            | without write access: placeholder metadata                           | `issue_new.templ`         |
| `milestones`        | `default`           | open and closed milestones; the Closed tab switches in the browser   | `milestones.templ`        |
| `milestones`        | `viewer`            | without write access: no New milestone button                        | `milestones.templ`        |
| `milestones`        | `empty`             | no milestones yet                                                    | `milestones.templ`        |
| `milestone_detail`  | `default`           | open milestone, open issues                                          | `milestone_detail.templ`  |
| `milestone_detail`  | `closed-issues`     | the Closed filter on the issues tab                                  | `milestone_detail.templ`  |
| `milestone_detail`  | `pulls`             | the pull requests tab                                                | `milestone_detail.templ`  |
| `milestone_detail`  | `closed`            | a closed milestone                                                   | `milestone_detail.templ`  |
| `milestone_detail`  | `viewer`            | without write access: no edit or delete                              | `milestone_detail.templ`  |
| `milestone_new`     | `default`           | empty form                                                           | `milestone_new.templ`     |
| `milestone_new`     | `error`             | the title was left blank                                             | `milestone_new.templ`     |
| `discussions`       | `default`           | open discussions, all categories                                     | `discussions.templ`       |
| `discussions`       | `answered`          | the Answered tab                                                     | `discussions.templ`       |
| `discussions`       | `closed`            | the Closed tab: locked, unanswered                                   | `discussions.templ`       |
| `discussions`       | `category`          | ?category=, one category selected                                    | `discussions.templ`       |
| `discussions`       | `viewer`            | without write access: no New discussion button                       | `discussions.templ`       |
| `discussions`       | `empty`             | a category with no open discussions                                  | `discussions.templ`       |
| `discussion_detail` | `default`           | open, for a maintainer                                               | `discussion_detail.templ` |
| `discussion_detail` | `answered`          | a reply is marked as the answer                                      | `discussion_detail.templ` |
| `discussion_detail` | `locked`            | locked: no reply box for a reader                                    | `discussion_detail.templ` |
| `discussion_detail` | `viewer`            | signed in without write access                                       | `discussion_detail.templ` |
| `discussion_detail` | `anon`              | signed out: sign in to reply                                         | `discussion_detail.templ` |
| `discussion_new`    | `default`           | category picked, empty form                                          | `discussion_new.templ`    |
| `discussion_new`    | `error`             | the title was left blank                                             | `discussion_new.templ`    |
| `discussion_new`    | `no-categories`     | the repository has no categories                                     | `discussion_new.templ`    |

<!-- mockgen:end -->

## Pages with no mock

| Real page               | Route                                    | Notes           |
| ----------------------- | ---------------------------------------- | --------------- |
| `explore.templ`         | `/explore`                               |                 |
| `setup.templ`           | `/setup`                                 | First-run setup |
| `oauth_authorize.templ` | `/oauth/authorize`                       |                 |
| `device_login.templ`    | `/login/device`, `/login/device/confirm` |                 |

## Pages covered by a differently named mock

| Mock                                     | Real page                                         |
| ---------------------------------------- | ------------------------------------------------- |
| `actions.html`                           | `checks.templ`: the repo Checks page              |
| `pr.html`                                | `pull_detail.templ`                               |
| `project.html`, `project_board.html`     | `projects.templ`, `project_detail.templ`          |
| `profile.html`, `org_profile.html`       | `user.templ`, `org.templ`                         |
| `settings.html`, `account_settings.html` | `repo_settings.templ`, `settings.templ`           |
| `new_repo.html`                          | `repo_new.templ`                                  |
| `my_repositories.html`, `my_pulls.html`  | `account_repos.templ`, `account_pulls.templ`      |
| `my_issues.html`, `attention.html`       | `account_issues.templ`, `account_attention.templ` |
| `wiki*.html` (six pages)                 | `wiki_page.templ`, `wiki_shared.templ`            |

## States with no mock

Mocks show the happy path. These real states are not drawn:

- **Empty states.** PR lists with no match; repo with no commits ("This repository is empty."), empty directory, no
  releases, no projects, no wiki page ("This page does not exist yet."), empty PR lists, no checks (the curl example).
- **Read-only and signed-out views.** On pull requests: the `pr.html` merge box's lock line ("Only collaborators with
  write access…") in place of Merge and Close.
- **PR states.** `pr.html` opens in other states through hashes: `#pr=open|draft|merged`,
  `#merge=ready|needs|checks-pending|failing|changes|conflicts|auto`, `#viewer=reviewer|author|readonly` (join with
  `&`).
- **Other states.** Closed pull requests; archived and mirror repo banners; binary file in `blob`; hidden-issue project
  card; form validation errors (`Alert` destructive) on every create page but `new_file.html` (submit a blank, `.git` or
  slash-ended path, or open `#error`); one-time token and backup-code reveals.
- **Requests in flight and failed.** Mocks send no requests, so no button shows the busy spinner (`aria-busy`, from
  `static/htmx-busy.js`) and no failed save shows its error toast. The styles are shared: setting `aria-busy="true"` on
  a mock button draws the real busy state.
- **Filters and navigation.** The command palette, organization people and repository tabs, and the profile README call
  to action.
- **Other workflows.** Pull-mirror settings, a pending transfer, home load warnings, and PR diff error states.
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
- **File deletion** in `blob.html`: confirmation shows feedback but leaves the file displayed.
- **Disabled in the real app too:** the Teams and org webhooks panels in `org_settings.html`.
- **Links to a page with no mock:** `org_profile.html` "Repositories" tab, and any link to a page in the table above.
- **Auth states:** the sign-in error banner, the email-first signup flow, expired and invalid reset or verify links, and
  the "instance can't send email" state.

## Deliberate differences from the real app

- **Project board cards.** Every card opens the card dialog, with the linked issue or PR shown as a link inside it. In
  the real app only note cards open the dialog; a linked issue or PR card navigates straight to that item.
- **PR checks page.** The repo tab bar highlights Checks. The real app keeps Pull requests highlighted on every PR tab.
