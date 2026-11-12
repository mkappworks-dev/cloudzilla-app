# Mock coverage

Which real pages (`internal/view/pages/*.templ`) have a mock, and which do not. Checked against the router and templates
in November 2026; update it when a mock is added.

## Generated pages

Rendered from the real templates by `cmd/mockgen` into `mockups/generated/`; see the [README](./README.md). A page that
cannot render from a plain view-model is listed under "Pages with no mock" with the reason.

<!-- mockgen:start -->

| Page               | Variant         | Shows                                                                  | Template                  |
| ------------------ | --------------- | ---------------------------------------------------------------------- | ------------------------- |
| `home`             | `default`       | signed in with repositories                                            | `home.templ`              |
| `home`             | `anon`          | signed out                                                             | `home.templ`              |
| `home`             | `empty`         | no repositories yet                                                    | `home.templ`              |
| `home`             | `admin-mode`    | instance admin acting with elevated rights                             | `home.templ`              |
| `login`            | `default`       | email and password; the instance cannot send email, so no reset link   | `login.templ`             |
| `login`            | `reset-link`    | the instance can send email                                            | `login.templ`             |
| `login`            | `error`         | wrong password                                                         | `login.templ`             |
| `login`            | `sso`           | LDAP and SAML enabled                                                  | `login.templ`             |
| `not_found`        | `default`       | signed in                                                              | `not_found.templ`         |
| `not_found`        | `anon`          | signed out                                                             | `not_found.templ`         |
| `register`         | `default`       | username, email and password                                           | `register.templ`          |
| `register`         | `error`         | username already taken                                                 | `register.templ`          |
| `register_email`   | `default`       | email-first signup, shown when the instance can send email             | `signup.templ`            |
| `register_email`   | `error`         | address is not an email                                                | `signup.templ`            |
| `register_email`   | `check-inbox`   | shown after every submit                                               | `signup.templ`            |
| `signup_complete`  | `default`       | choose a username and password from the emailed link                   | `signup.templ`            |
| `signup_complete`  | `error`         | username already taken                                                 | `signup.templ`            |
| `signup_complete`  | `invalid`       | link used or expired                                                   | `signup.templ`            |
| `forgot_password`  | `default`       | email form                                                             | `password_reset.templ`    |
| `forgot_password`  | `sent`          | the page every valid address gets                                      | `password_reset.templ`    |
| `forgot_password`  | `error`         | address is not an email                                                | `password_reset.templ`    |
| `forgot_password`  | `unavailable`   | the instance cannot send email                                         | `password_reset.templ`    |
| `reset_password`   | `default`       | pending link                                                           | `password_reset.templ`    |
| `reset_password`   | `totp`          | pending link on an account with two-factor                             | `password_reset.templ`    |
| `reset_password`   | `error`         | passwords differ                                                       | `password_reset.templ`    |
| `reset_password`   | `expired`       | link older than its lifetime                                           | `password_reset.templ`    |
| `reset_password`   | `invalid`       | link used, replaced or revoked                                         | `password_reset.templ`    |
| `verify_email`     | `default`       | pending link, signed in                                                | `verify_email.templ`      |
| `verify_email`     | `verified`      | after confirming                                                       | `verify_email.templ`      |
| `verify_email`     | `expired`       | link older than 24 hours                                               | `verify_email.templ`      |
| `verify_email`     | `invalid`       | link used, replaced or for an address the account dropped, signed out  | `verify_email.templ`      |
| `totp_verify`      | `default`       | authenticator code                                                     | `totp_verify.templ`       |
| `totp_verify`      | `error`         | wrong code                                                             | `totp_verify.templ`       |
| `invite`           | `default`       | valid invitation                                                       | `invite.templ`            |
| `invite`           | `error`         | username already taken                                                 | `invite.templ`            |
| `invite`           | `invalid`       | link used or expired                                                   | `invite.templ`            |
| `profile`          | `default`       | your own profile                                                       | `user.templ`              |
| `profile`          | `other`         | someone else's profile                                                 | `user.templ`              |
| `profile`          | `empty`         | new account with nothing yet                                           | `user.templ`              |
| `org_profile`      | `default`       | owner viewing their organization                                       | `org.templ`               |
| `org_profile`      | `visitor`       | signed-out visitor                                                     | `org.templ`               |
| `org_profile`      | `empty`         | nothing pinned, no repositories                                        | `org.templ`               |
| `organizations`    | `default`       | organizations you belong to                                            | `organizations.templ`     |
| `organizations`    | `empty`         | no organizations yet                                                   | `organizations.templ`     |
| `new_organization` | `default`       | empty form                                                             | `new_organization.templ`  |
| `new_organization` | `error`         | name already taken                                                     | `new_organization.templ`  |
| `my_repositories`  | `default`       | all repositories                                                       | `account_repos.templ`     |
| `my_repositories`  | `transfers`     | a repository transfer is waiting                                       | `account_repos.templ`     |
| `my_repositories`  | `empty`         | no repositories yet                                                    | `account_repos.templ`     |
| `my_pulls`         | `default`       | created by you, open                                                   | `account_pulls.templ`     |
| `my_pulls`         | `empty`         | nothing open                                                           | `account_pulls.templ`     |
| `my_issues`        | `default`       | assigned to you, open                                                  | `account_issues.templ`    |
| `my_issues`        | `empty`         | nothing open                                                           | `account_issues.templ`    |
| `attention`        | `default`       | items waiting on you                                                   | `account_attention.templ` |
| `attention`        | `empty`         | nothing waiting                                                        | `account_attention.templ` |
| `notifications`    | `default`       | inbox with unread items                                                | `notifications.templ`     |
| `notifications`    | `empty`         | inbox zero                                                             | `notifications.templ`     |
| `stars`            | `default`       | starred repositories                                                   | `account_stars.templ`     |
| `stars`            | `empty`         | nothing starred                                                        | `account_stars.templ`     |
| `new_repo`         | `default`       | create form                                                            | `repo_new.templ`          |
| `repo_import`      | `default`       | import form                                                            | `repo_import.templ`       |
| `repo_import`      | `mirror`        | the instance allows pull mirrors                                       | `repo_import.templ`       |
| `repo_import`      | `running`       | clone in progress                                                      | `repo_import.templ`       |
| `repo_import`      | `failed`        | clone failed                                                           | `repo_import.templ`       |
| `repo_transfers`   | `default`       | transfers offered to you                                               | `repo_transfers.templ`    |
| `repo_transfers`   | `empty`         | none waiting                                                           | `repo_transfers.templ`    |
| `account_settings` | `default`       | profile, keys, tokens and security                                     | `settings.templ`          |
| `account_settings` | `fresh`         | new account: no keys, tokens or apps, two-factor off, email unverified | `settings.templ`          |
| `account_settings` | `error`         | profile save failed                                                    | `settings.templ`          |
| `account_settings` | `unverified`    | email not yet verified, the instance can send email                    | `settings.templ`          |
| `account_settings` | `totp-setup`    | two-factor enrolment in progress                                       | `settings.templ`          |
| `account_settings` | `new-token`     | a token was just created                                               | `settings.templ`          |
| `org_settings`     | `default`       | members, defaults and danger zone                                      | `org_settings.templ`      |
| `org_settings`     | `no-repos`      | organization with no repositories                                      | `org_settings.templ`      |
| `explore`          | `default`       | trending repositories                                                  | `explore.templ`           |
| `explore`          | `anon`          | signed out                                                             | `explore.templ`           |
| `explore`          | `empty`         | nothing to show                                                        | `explore.templ`           |
| `setup`            | `default`       | first-run admin account                                                | `setup.templ`             |
| `setup`            | `error`         | a required field is blank                                              | `setup.templ`             |
| `oauth_authorize`  | `default`       | consent screen                                                         | `oauth_authorize.templ`   |
| `oauth_authorize`  | `error`         | redirect URI rejected                                                  | `oauth_authorize.templ`   |
| `device_login`     | `default`       | enter the code                                                         | `device_login.templ`      |
| `device_login`     | `error`         | unknown code                                                           | `device_login.templ`      |
| `device_login`     | `confirm`       | approve the request                                                    | `device_login.templ`      |
| `device_login`     | `confirm-error` | wrong password                                                         | `device_login.templ`      |
| `device_login`     | `approved`      | after approving                                                        | `device_login.templ`      |
| `device_login`     | `denied`        | after denying                                                          | `device_login.templ`      |
| `admin_settings`   | `default`       | site settings and invitations                                          | `admin_settings.templ`    |
| `admin_settings`   | `admin-mode`    | instance admin acting with elevated rights                             | `admin_settings.templ`    |
| `admin_settings`   | `empty`         | no invitations sent                                                    | `admin_settings.templ`    |
| `admin_users`      | `default`       | first page of users                                                    | `admin_users.templ`       |
| `admin_users`      | `empty`         | filter matches nobody                                                  | `admin_users.templ`       |
| `admin_user`       | `default`       | another active user                                                    | `admin_users.templ`       |
| `admin_user`       | `sole-owner`    | user who is the only owner of an organization                          | `admin_users.templ`       |
| `admin_user`       | `self`          | the admin's own account offers no actions                              | `admin_users.templ`       |
| `admin_user`       | `suspended`     | suspended user                                                         | `admin_users.templ`       |
| `audit_log`        | `default`       | recent entries                                                         | `audit_log.templ`         |
| `audit_log`        | `empty`         | no entries match                                                       | `audit_log.templ`         |
| `sso_settings`     | `default`       | nothing configured                                                     | `sso_settings.templ`      |
| `sso_settings`     | `configured`    | LDAP on, SAML saved but off                                            | `sso_settings.templ`      |
| `sso_settings`     | `error`         | save failed                                                            | `sso_settings.templ`      |
| `admin_mode`       | `default`       | not in admin mode                                                      | `admin_mode.templ`        |
| `admin_mode`       | `active`        | in admin mode, with the banner                                         | `admin_mode.templ`        |
| `forbidden`        | `default`       | signed in                                                              | `forbidden.templ`         |
| `forbidden`        | `anon`          | signed out                                                             | `forbidden.templ`         |
| `forbidden`        | `admin-hint`    | superadmin who could open the page in admin mode                       | `forbidden.templ`         |

<!-- mockgen:end -->

## Pages with no mock

Every account, organization, auth and admin page renders from a plain view-model, so none is skipped. Pages of other
families that cannot are listed here.

## Pages covered by a differently named mock

| Mock                                      | Real page                                        |
| ----------------------------------------- | ------------------------------------------------ |
| `actions.html`                            | `checks.templ`: the repo Checks page             |
| `pr.html`                                 | `pull_detail.templ`                              |
| `project.html`, `project_board.html`      | `projects.templ`, `project_detail.templ`         |
| `settings.html`                           | `repo_settings.templ`                            |
| `wiki*.html` (six pages)                  | `wiki_page.templ`, `wiki_shared.templ`           |
| `discussion_detail_locked.html`           | The locked state of `discussion_detail.templ`    |
| `issues_closed.html`, `*_closed/answered` | Tab states of the list pages                     |
| `issues_filtered.html`                    | `issues.templ` with filter chips and a menu open |

## States with no mock

Mocks show the happy path. These real states are not drawn:

- **Empty states.** Issue and PR lists with no match ("No open issues match these filters."); repo with no commits
  ("This repository is empty."), empty directory, no releases, no milestones, no projects, no wiki page ("This page does
  not exist yet."), empty discussion and PR lists, no checks (the curl example).
- **Unrecognized qualifier notice.** A list search holding a token such as `sort:comments-desc` shows a line under the
  search field saying it was searched as text (`ListFilterBar`'s `Notices`).
- **Read-only and signed-out views.** No Edit, Delete or Manage controls; "Sign in to reply."; a locked issue the viewer
  cannot comment on; the `pr.html` merge box's lock line ("Only collaborators with write access…") in place of Merge and
  Close.
- **PR states.** `pr.html` opens in other states through hashes: `#pr=open|draft|merged`,
  `#merge=ready|needs|checks-pending|failing|changes|conflicts|auto`, `#viewer=reviewer|author|readonly` (join with
  `&`).
- **Other states.** Closed pull requests; archived and mirror repo banners; binary file in `blob`; hidden-issue project
  card; form validation errors (`Alert` destructive) on `new_file.html` (submit a blank, `.git` or slash-ended path, or
  open `#error`); the one-time backup-code reveal.
- **Requests in flight and failed.** Mocks send no requests, so no button shows the busy spinner (`aria-busy`, from
  `static/htmx-busy.js`) and no failed save shows its error toast. The styles are shared: setting `aria-busy="true"` on
  a mock button draws the real busy state.
- **Filters and navigation.** `?category=` on discussions, the command palette, organization people and repository tabs,
  and the profile README call to action.
- **Other workflows.** Pull-mirror settings, a pending transfer, home load warnings, the issue template picker, and PR
  diff error states.
- **Admin mode.** Pages with an `admin-mode` variant draw the "Acting as instance admin" banner and drop "Enter admin
  mode" from the user menu. Not drawn: the identity dialog that "Enter admin mode" opens.
- **Admin.** The identity-confirm dialog (password, two-factor and provider factors), the SSO success banner, and users
  and audit-log pages beyond the first. The mocks use the generic confirm dialog instead.

## Differences found in the real app

Seen while porting, not fixed:

- A closed discussion has no UI: no banner, no Reopen, no "closed this" event. Only the locked state exists.

## Controls that are mocked or not wired

Server-side actions are faked in the browser: removing a collaborator, label, rule, webhook, deploy key or org member,
revoking a key, webhook deliveries, archive and unarchive, leaving an org, deleting a comment, accepting or declining a
transfer, create and delete branch or tag, and Watch and Star toggles. They use the real confirm dialogs, toasts and
empty states.

Some controls and forms have no behaviour. In particular:

- **Add forms and saves** in `settings.html` (repo), the `org_settings` and `account_settings` variants (Add key, label,
  rule and webhook, Save changes, member Role selects, and Save profile): these do not persist changes; several forms
  suppress submission without feedback.
- **File deletion** in `blob.html`: confirmation shows feedback but leaves the file displayed.
- **Disabled in the real app too:** the Teams and org webhooks panels on the org settings page.
- **Links to a page with no mock:** the `org_profile` "Repositories" tab, and any link to a page in the table above.

## Deliberate differences from the real app

- **Project board cards.** Every card opens the card dialog, with the linked issue or PR shown as a link inside it. In
  the real app only note cards open the dialog; a linked issue or PR card navigates straight to that item.
- **PR checks page.** The repo tab bar highlights Checks. The real app keeps Pull requests highlighted on every PR tab.
