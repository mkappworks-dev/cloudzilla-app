# 2026-10-04 — Fork page

**Status:** Design approved.
**Branch:** `feat/fork-page`
**Affected subsystems:** `RepoService.Fork` and the shared owner resolver (`internal/service/`), `RepoStore.Fork` and a new fork lookup (`internal/store/repo_store.go`), `ForkRepo` handler and a new page handler, a new Templ page, `repo.templ`'s Fork button, router, `docs/api-reference.md`.

---

## Background

The Fork button on a repo page posts straight to `POST /api/repos/{owner}/{repo}/fork`. That copies the whole bare repo into the viewer's personal account under the source's name, or `name-1`, `name-2`, … while the name is taken. The user can't choose the owner, name or description, can't fork into an org, and can't leave out branches. Nothing says when they already have a fork.

GitHub puts a "Create a new fork" form in front of the copy. This design does the same.

## Locked decisions

1. **One form page, then the existing API.** `GET /{owner}/{repo}/fork` shows the form. It submits JSON to the existing `POST /api/repos/{owner}/{repo}/fork`, the same way `/repos/new` submits to `POST /api/repos/`.
2. **Owner is the viewer's account or an org they own.** This is the same rule as `/repos/new` and imports. Org members who aren't owners can't create org repos anywhere else either.
3. **No fork into the source's own namespace.** The page leaves the source's owner out of the dropdown, and the service refuses it with `ErrForkIntoSourceOwner`. A fork there is just a copy, and GitHub refuses it too. This changes the API: today an owner can fork their own repo into `name-1`.
4. **Visibility follows the source.** A fork of a private repo stays private, and a fork of a public repo stays public, as today. The page shows it read-only.
5. **"Copy the default branch only" is on by default**, as on GitHub. It deletes every `refs/heads/*` except the default branch after the copy and points the fork's HEAD at that branch. Tags are kept. A default branch that isn't one of the source's branches is refused with `ErrForkDefaultBranchMissing`: Settings accept any string, and pruning to a missing branch would leave a fork with none. Unreachable objects stay in the pack: there is no pure-Go `gc` here, and today's full copy already holds them.
6. **The existing-fork notice is informational.** It lists the viewer's forks of this repo (their account plus orgs they own) and doesn't stop a second fork under another name or owner.
7. **The API stays backward compatible.** With no body, or an empty one, the API forks into the caller's account under the source's name with the `-N` fallback and copies all branches, as today.

---

## Service and store

### `ForkOptions`

```go
type ForkOptions struct {
	Owner             string  // "" = the actor's account
	Name              string  // "" = the source's name, suffixed -1, -2, … while taken
	Description       *string // nil = the source's description
	DefaultBranchOnly bool
}

func (s *RepoService) Fork(ctx context.Context, originalOwner, originalName string, actorID int64, actorUsername string, opts ForkOptions) (*model.Repository, error)
```

The zero `ForkOptions` is today's behaviour. Every existing caller (the handler and the tests in `repo_dirs_test.go` and `repo_fork_test.go`) passes `service.ForkOptions{}`.

### Steps

1. Load the source and check `CanRead`, as today.
2. Resolve `opts.Owner` with the shared owner resolver (below), which returns `ErrForbidden` for an org the actor doesn't own.
3. If the resolved namespace is the source's (`strings.EqualFold` on owner names), return `ErrForkIntoSourceOwner`.
4. Pick the name:
   - **`opts.Name` set:** check it with `ValidateRepoName`, which wraps `ErrInvalidRepoName`, then `claimRepo` once. A clash returns `ErrRepoNameTaken` or `ErrRepoNameReserved`, with no suffix.
   - **`opts.Name` empty:** today's `-N` loop.
5. Insert the row (see "`RepoStore.Fork`" below), then `copyDir`. Any failure calls `abandonNewRepo`, as today.
6. If `opts.DefaultBranchOnly` is set, prune the copy's branches (see "Pruning branches" below). On failure, `abandonNewRepo` and return the error.
7. Run `IncrementForkCount` as today. Set `ForkOfOwner` and `ForkOfName` on the result.

### Shared owner resolver

`ImportTarget`, `ResolveImportTarget` and `errImportOwner` in `repo_import.go` become `RepoTarget`, `ResolveRepoTarget` and `errTargetOwner`. They move to `repo_service.go` beside `personalOwner`. The error text becomes neutral: `you can create repositories only in your account or an organization you own`.

The import handler already writes its own 403 message, so imports look the same to users. Import callers and tests are renamed mechanically. The historical plan in `docs/superpowers/plans/2026-10-03-repo-import.md` is left as it is.

### `RepoStore.Fork`

`Fork(ctx, r *model.Repository) error` takes a row the service has filled in: `OwnerID` or `OrgID`, `CreatedBy` (the actor), `OwnerName`, `Name`, `Description`, `Private`, `DefaultBranch`, and `ForkOfID`.

The insert stores `org_id` and `created_by` through `nullID`, as `CreateWithOwnerName` does, with `is_fork = TRUE`. Today it sets neither column: `created_by` gets the owner's ID, and an org fork would have no `org_id`, so org owners couldn't manage it.

### Pruning branches

`pruneToDefaultBranch(gitDir, defaultBranch string) error` in `repo_service.go` opens the copy with go-git. It removes every reference under `refs/heads/` except `refs/heads/<defaultBranch>`, using `Storer.RemoveReference`, which handles both loose and packed refs. Tags are left alone. It then sets HEAD to a symbolic ref to `refs/heads/<defaultBranch>`, so the fork's HEAD matches its `default_branch` column even when the source's HEAD points elsewhere.

If `refs/heads/<defaultBranch>` is missing while other branches exist, it returns `ErrForkDefaultBranchMissing` before removing anything. `Fork` then runs `abandonNewRepo` and returns the sentinel unwrapped, and the handler answers 422 with its text. An empty source (no branches at all) has nothing to prune and is not refused.

### `ForksOwnedBy`

- **Service:** `RepoService.ForksOwnedBy(ctx, repoID, userID int64) ([]model.Repository, error)`.
- **Store:** `RepoStore.ListForksOwnedBy`, which selects `fork_of_id = $1 AND deleted_at IS NULL AND ownedBy("repositories", "$2")`, ordered by `owner_name, name`, with the same columns as `ListForks`. Using `ownedBy` keeps a popular repo's forks out of Go memory.

### Errors

- **New sentinel:** `ErrForkIntoSourceOwner = errors.New("a repository can't be forked into the account or organization that owns it")`.
- **Reused:** `ErrRepoNameTaken`, `ErrRepoNameReserved`, `ErrInvalidRepoName` and `ErrForbidden`.

---

## API: `POST /api/repos/{owner}/{repo}/fork`

The route stays inside the `/api/repos` group (`optAuthMW`, `apiBodyLimit`) with `authMW`.

**Body.** It is parsed as JSON only when `Content-Type` starts with `application/json`. Any other body, including the OAuth test's empty form post, means zero options.

```json
{"owner": "acme", "name": "widgets", "description": "…", "default_branch_only": true}
```

`description` is a pointer, so a missing key keeps the source's description and `""` clears it. Malformed JSON gets a 400 `invalid request body`.

**Token targets.** The middleware checks a target-limited token only against the path, which names the source repo. So when `claims.Targets` is non-empty and the resolved owner is an org, the handler requires the org's name to be one of the targets (case-insensitive, as `TargetAllows` does). Otherwise it returns 403 `this token isn't allowed for that repository or organization`, the middleware's wording. Forks into the token user's own account stay allowed, as today.

**Responses:**

| Case | Status | Body |
| --- | --- | --- |
| Source missing or unreadable | 404 | `repo not found` (from `readableRepoJSON`, unchanged) |
| `ErrForbidden` (org not owned) | 403 | `you can fork only into your account or an organization you own` |
| `ErrForkIntoSourceOwner` | 422 | the error's text |
| `ErrRepoNameTaken`, `ErrRepoNameReserved` | 422 | the error's text |
| `ErrInvalidRepoName` | 422 | `invalidRepoNameMessage` |
| `ErrInvalidRepoPath` | 422 | `unsafeRepoPathMessage` |
| Success, JSON request | 201 | `{"owner": …, "name": …, "url": "/owner/name"}` |
| Success, HTMX request | 200 | `HX-Redirect`, unchanged |
| Success, other requests | 303 | redirect, unchanged |

On success the `EventFork` event's `fork_owner` metadata becomes the chosen owner, not always the actor.

## Page: `GET /{owner}/{repo}/fork`

- **Route:** `r.With(authMW).Get("/{owner}/{repo}/fork", h.PageForkRepo)`, next to `/{owner}/{repo}/settings`.
- **Handler:** `PageForkRepo` in `internal/handler/page_fork_handler.go` loads the source with `readableRepo`, so an unreadable repo gets the normal 404. It collects:
  - **Owner options:** the viewer's account plus `Org.ListOwnedByUser`, minus the source's owner.
  - **Existing forks:** `Repo.ForksOwnedBy(source.ID, viewer)`. A failed lookup is logged and leaves the list empty.
- **View-model:** `view.RepoForkData` in `viewmodels_repo.go`:

```go
type RepoForkData struct {
	BasePage
	SourceOwner   string
	SourceName    string
	Description   string
	Private       bool
	DefaultBranch string   // "" unless that branch exists (`CodeService.HasBranch`): the checkbox is hidden
	Owners        []string // in display order; empty when no namespace qualifies
	ExistingForks []RepoRef
}
```

- **Template:** `internal/view/pages/repo_fork.templ` uses `layout.Base(data.BasePage, "Fork "+owner+"/"+name)`, with the same two-column layout and form classes as `repo_new.templ`. The form has:
  - A header: "Create a new fork", a sentence that a fork is a copy of `owner/repo`, and a link to the source.
  - **Existing-fork notice:** shown when `ExistingForks` is non-empty. A muted bordered box reads "You already have a fork of this repository:" followed by links to each fork.
  - **Owner and name row:** the owner uses `repoNewCustomSelect`, with options built from `Owners` and no visibility data. Then `/`, then the name input, prefilled with the source name and using the same `pattern` as New repo.
  - **Description:** a textarea, prefilled.
  - **Visibility:** a read-only line reading "Public" or "Private", with "Forks keep the visibility of the repository they are forked from."
  - **Default branch only:** checked by default, labelled "Copy the `main` branch only". Help text: "Leave unchecked to copy every branch. Tags are copied either way." Hidden when `DefaultBranch` is empty.
  - **Error box:** `role="alert"` and hidden by default, as on New repo.
  - **Buttons:** Cancel (back to the source) and "Create fork" (`ButtonSuccess`).
- **No owner available:** when `Owners` is empty (the viewer owns the source and no orgs), the form is replaced by a short message: "You can't fork your own repository into your own account. Create an organization to fork it there." The message links to `/organizations/new`.
- **Script:** an inline `<script>`, as on `repo_new.templ`. It posts JSON with the `X-CSRF-Token` header, navigates to the response's `url` on 201, and otherwise shows the error text in the alert box. The submit button is disabled while the request is in flight.

## Fork button

In `repo.templ`, the signed-in branch of `#fork-button` becomes an `<a href="/{owner}/{repo}/fork">` with the same classes, icon and label, and no `hx-*` attributes. The signed-out link becomes `view.WithNext("/login", "/{owner}/{repo}/fork")`, so signing in lands on the form. The count is unchanged.

`fragments/fork_button.templ` and `view.ForkButtonData`, plus its alias in `handler/viewmodels.go`, are never rendered, so they're deleted.

## Docs

`docs/api-reference.md`: update both fork rows and add the body, the status table above, the source-owner rule and the token-target rule.

## Testing

**Service** (`internal/service/repo_fork_options_test.go`, drafted):
- A fork into an owned org with a new name and description gets the org's `org_id`, `created_by` set to the actor, the source's privacy, `fork_of_id`, a matching HEAD and a fork count of 1.
- An org the actor doesn't own, or where they're only a member, is `ErrForbidden` and leaves no row.
- The source's own namespace (personal with `""`, personal by name, and org into the same org) is `ErrForkIntoSourceOwner`. An org repo forked into the owner's account still works.
- A taken explicit name is `ErrRepoNameTaken` with no `-1` fallback. A path as the name is `ErrInvalidRepoName`.
- `DefaultBranchOnly`, with loose and with packed refs, leaves `main` plus tag `v1`, and HEAD on `main`. A full fork keeps `feature` too. The source keeps all its refs. With the settings default changed to `develop`, the fork keeps `develop` plus the tags and its HEAD resolves to `develop`. A default that names no branch is `ErrForkDefaultBranchMissing`, with no row, no directory and no fork-count change. An empty source forks fine.
- `ForksOwnedBy` returns the personal and owned-org forks, and skips another user's forks, forks in an org where the viewer is only a member, and soft-deleted forks.

**Existing tests** pass `service.ForkOptions{}` and keep passing unchanged. The import tests are renamed with the resolver.

**Handler** (`internal/handler/fork_handler_test.go`, new):
- A JSON body with owner, name and description creates the fork and returns 201 with `url`.
- An empty form post still forks into the caller's account, which the OAuth test also covers.
- Malformed JSON is a 400.
- Forking into the source's owner is a 422.
- A target-limited token naming an org outside its targets is a 403 and creates nothing, and the same token forking into its user's account still works.

**Page:** a router page check that `GET /{owner}/{repo}/fork` returns 200 with the form for a reader, 404 for a private repo the viewer can't read, a redirect to login when signed out, the existing-fork notice when the viewer has a fork, and the no-owner message for a sole owner without orgs.

**Browser:** run a throwaway server on a scratch DB. Fork a seeded repo into an org with "default branch only" checked, and check that the fork's branch list shows only `main`. Then check the existing-fork notice appears on a second visit, and that a name clash shows the inline error.
