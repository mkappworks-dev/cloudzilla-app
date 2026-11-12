# Unfinished settings surfaces

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent

Parent: ticket 34 of [`../2026-11-06-uiux-desktop-9.5/`](../2026-11-06-uiux-desktop-9.5/issues/34-unfinished-settings-surfaces.md).
Every UI ticket here follows the rules in that effort's [spec.md](../2026-11-06-uiux-desktop-9.5/spec.md): update the
standing mocks in `mockups/` in the same PR, and check the mock and the real page at 1440px in light and dark.

## Problem

Settings pages show controls for features that do not exist: disabled "Not available" and "Billing not yet supported"
radios on organization creation, "Coming soon" rows for additional emails and account export, an organization Teams
section and Webhooks section that only explain they are empty, and a read-only organization name. A forge with
organizations is expected to group members and grant repositories to the group.

## Decisions

Settled in the grilling session (2026-11-12). Terms are from `GLOSSARY.md`.

| Surface                   | Decision                                                                                       |
| ------------------------- | ---------------------------------------------------------------------------------------------- |
| Organization creation     | Drop the enterprise and Team-plan radios and their fieldsets. No billing or enterprise concept |
| Fork Owner default        | Not a bug: the User is already first. Show the User's account as a disabled option with a reason when they own the source |
| Organization name change  | Build, after repository rename (ticket 15) lands its alias mechanism                           |
| Additional emails         | Build                                                                                          |
| Account export            | Build                                                                                          |
| Organization webhooks     | Build, reusing `docs/webhooks.md`                                                              |
| Teams                     | Build, in two PRs: membership first, then repository grants and permission resolution          |
| Repository rename         | Out of scope: ticket 15. `repo_settings.templ`'s rename row stays with 15                      |

### Fork Owner

`PageForkRepo` (`internal/handler/fork_page_handler.go`) already puts the current User first. It omits them only when
they own the source Repository, because `ErrForkIntoSourceOwner` forbids that fork. Then an Organization is first and
nothing says why the User is missing. Fix: keep the User's account in the Owner list, disabled, with "Owns this
repository"; the first enabled option is the default. Keep the existing empty state for a User with no Organization to
fork into. Same pattern as GitHub and GitLab, which keep unusable namespaces visible and disabled with the reason.

### Organization name change

Reuses the alias mechanism of ticket 15 (an old path redirects until another repository claims the name, which then wins
and deletes the alias), one level up: an old Organization name maps to the renamed Organization, so `/old/x` redirects
to `/new/x` for every Repository, over HTTP and SSH.

- Only an Organization `owner`, after the confirmation dangerous actions already use. The name follows the creation
  rule and the shared User/Organization name space (case-insensitive, reserved words).
- One transaction renames the row and creates the alias; the repository and wiki directories under
  `<repos_root>/<owner>/` move with it. Webhooks, stars and issues key on ids and stay valid. Check the avatar storage
  keys.
- Open question for the ticket 15 session: does its alias table support owner-level aliases? The ticket is blocked by 15.

### Additional emails

- A `user_emails` table. A new address needs a verification link by email. Only verified addresses credit commits:
  `userByAuthorEmail` (`internal/service/commit_email.go`) falls back to them after the primary address.
- An address is unique across all Users, primary or additional, case-insensitive (the `users_email_lower_key` rule).
- Adding and removing need the same re-authentication as changing the primary email.
- Extra addresses only credit commits. They are not used for sign-in, notifications or password reset.
- Without SMTP the section hides "Add email" and says verification needs email. The primary address cannot be removed;
  promoting an extra address is out of scope.

### Account export

- "Request export" starts a background job (one running per User, at most one request per day). It writes a zip through
  the storage `Backend` (`docs/storage.md`), key under `exports/`.
- Contents are JSON: profile, public SSH keys, issues and comments, pull requests and reviews the User wrote, gists with
  their files, stars and watches. Not included: repository and wiki git data (the danger zone already says to clone
  first), tokens, password hashes, TOTP secrets.
- The row shows requested, ready (download link) or failed. The file is deleted after 7 days and on account deletion.
  Download needs re-authentication. With SMTP on, an email says when it is ready.

### Organization webhooks

- One webhook on an Organization receives `push`, `issues` and `pull_request` from every Repository it owns, including
  later ones, with the repository payloads unchanged.
- Delivery is reused as is: HMAC signing, the retry schedule, `dialPublic`, `webhook_deliveries`.
- Schema: `webhooks.repo_id` becomes nullable, new nullable `org_id`, CHECK that exactly one is set. `Dispatch` also loads
  the hooks of the repository's Organization.
- Only an Organization `owner` creates, lists and deletes them; a repository `admin` cannot. Repository settings shows,
  read-only, that an organization webhook also fires (no URL or secret).
- API under `/api/orgs/{org}/hooks`, `repo:admin` token scope like the repository one. A Repository transferred out
  stops firing the Organization's hooks; one transferred in starts.

### Teams

A **Team** is a named group of Organization members that holds Repository roles.

- Holds Repository grants only, with `reader`, `writer` or `admin`. The Org role stays `owner` or `member`.
- Flat (no nesting): name, slug unique within the Organization, optional description.
- Only an Organization `owner` creates, renames and deletes Teams, edits members and grants Repositories. A `team`
  `admin` grant is therefore owner-only, like a Collaborator `admin`.
- Only current Organization members join. Removing someone from the Organization removes them from every Team. Deleting
  a Team deletes its grants. Any Organization member can see the Team list and members; grants on private Repositories
  show only to an `owner`.

**Permission resolution.**

- Effective role = the highest of the direct Collaborator role and every grant of every Team the User belongs to
  (`reader` < `writer` < `admin`).
- `CanRead` (private): any effective role. `CanWrite`: `writer` or `admin`. `CanManage`: `admin`. These are today's
  checks with the effective role in place of the direct role.
- `IsOwner` does not change. A `team` `admin` manages settings and `reader`/`writer` Collaborators but cannot grant or
  revoke `admin`, transfer, archive or delete.
- Removing a direct Collaborator row does not remove Team access, and the reverse. The collaborator list shows "via team
  `x`" lines so an owner sees why access remains.
- `readableBy` (`internal/store/repo_store.go`) gains an `EXISTS` over Team grants; `ownedBy` is unchanged. Callers of
  both are listed in ticket 06. `issueVisibleTo` follows `CanWrite`. Git HTTP and SSH, the API and tokens call the same
  checks; token scopes still narrow.
- A public Repository keeps its public read. Transferring a Repository out of the Organization deletes its Team grants.

## Tickets

Order is cheapest first. Rows sharing a file wait for each other; the rest are parallel-safe.

| #   | Ticket                                                    | Blocked by | Shared files                                              |
| --- | --------------------------------------------------------- | ---------- | --------------------------------------------------------- |
| 01  | [Remove dead controls](issues/01-remove-dead-controls.md)  | —          | `new_organization.templ`, `repo_fork.templ`, `repoNewCustomSelect` |
| 02  | [Additional emails](issues/02-additional-emails.md)        | —          | `settings.templ`                                          |
| 03  | [Account export](issues/03-account-export.md)              | 02         | `settings.templ`                                          |
| 04  | [Organization webhooks](issues/04-organization-webhooks.md) | —         | `org_settings.templ`                                      |
| 05  | [Teams: create and members](issues/05-teams-members.md)    | 04         | `org_settings.templ`                                      |
| 06  | [Teams: grants and permission resolution](issues/06-teams-grants-and-resolution.md) | 05 | repository settings, `repo_store.go` |
| 07  | [Organization name change](issues/07-organization-name-change.md) | 05, ticket 15 | `org_settings.templ`                       |

Parallel-safe: 01, 02 and 04 start together; 03 follows 02; 05 → 06 and 05 → 07 follow 04. 07 is also blocked by ticket
15 of the uiux effort (alias mechanism). Migrations are numbered at build time: re-check the sequence after merging
`main`.

The "Coming soon" helpers (`comingSoonBadge`, `comingSoonButton` in `settings.templ`) are deleted by whichever of 02 and
03 merges last.

## Out of scope

Repository rename (ticket 15). Billing, plans, enterprise accounts. Nested Teams, Team-level Organization rights,
promoting an additional email to primary, sign-in or notifications on additional emails, repository data in the export.

## Comments
