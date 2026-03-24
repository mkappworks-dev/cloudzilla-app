# Code Browser & Refs

## URL Patterns

| View                    | Route                                     |
| ----------------------- | ----------------------------------------- |
| Root tree               | `/{owner}/{repo}/tree/{ref}`              |
| Subtree                 | `/{owner}/{repo}/tree/{ref}/{path...}`    |
| Blob                    | `/{owner}/{repo}/blob/{ref}/{path...}`    |
| Blame                   | `/{owner}/{repo}/blame/{ref}/{path...}`   |
| Commit log              | `/{owner}/{repo}/commits/{ref}`           |
| Commit log (file scope) | `/{owner}/{repo}/commits/{ref}/{path...}` |
| Single commit diff      | `/{owner}/{repo}/commit/{sha}`            |
| Branches & Tags         | `/{owner}/{repo}/refs`                    |

`{ref}` = branch name, tag name, or commit SHA. Pagination via `?page=N` (1-indexed, 30 per page).

---

## CodeService (`internal/service/code_service.go`)

`CodeService` has **no store dependency** — it reads git data directly from bare repos on disk via go-git. It is wired in `services.New()` and receives `config.GitConfig` (for `ReposRoot`).

### Key Methods

- `ResolveRef(owner, repoName, ref)` → `(*object.Commit, displayRef, error)`
- `GetTree(owner, repoName, ref, path)` → `*TreeResult`
- `GetBlob(owner, repoName, ref, path)` → `*BlobResult`
- `GetBlame(owner, repoName, ref, path)` → `*BlameResult`
- `GetCommits(owner, repoName, ref, page, pageSize)` → `*CommitLog`
- `GetCommit(owner, repoName, sha)` → `*CommitDetail`
- `ListRefs(owner, repoName, defaultBranch)` → `*RefsResult`
- `CreateBranch(owner, repoName, name, fromRef)` → `error`
- `DeleteBranch(owner, repoName, name)` → `error`
- `CreateTag(owner, repoName, name, fromRef)` → `error`
- `DeleteTag(owner, repoName, name)` → `error`

### ResolveRef Priority

1. Branch: `repo.Reference(plumbing.NewBranchReferenceName(ref), true)`
2. Tag: `repo.Reference(plumbing.NewTagReferenceName(ref), true)`
3. Raw SHA: `repo.CommitObject(plumbing.NewHash(ref))`
4. HEAD fallback (when `ref == ""`): `repo.Head()`

Returns `ErrEmptyRepo` sentinel when HEAD resolution fails (repo has no commits). Handlers return 404 on this error.

### Result Types

- `TreeResult` — `Entries []TreeEntry` (dirs first, then files, both sorted), `Ref`, `Path`, `Breadcrumbs`
- `BlobResult` — `Lines []CodeLine`, `IsBinary bool`, `BlameURL`, breadcrumbs
- `BlameResult` — `Lines []BlameLine` with `ShowMeta bool` (true when commit run changes), `BlobURL`, breadcrumbs
- `CommitLog` — `Commits []CommitSummary`, `Ref`, `Page`, `PrevPage`, `NextPage`, `HasMore`
- `CommitDetail` — full commit with `Files []FileDiff` (hunks with add/del/ctx lines), `TotalAdded`, `TotalDeleted`
- `RefsResult` — `Branches []BranchInfo` (`Name`, `Hash`, `IsDefault`), `Tags []TagInfo` (`Name`, `Hash`)

---

## Branch & Tag Management

The Refs page (`/{owner}/{repo}/refs`) lists all branches and tags. Authenticated users with write access can create and delete branches/tags via HTMX forms.

**Permission rules:**

- Public repos: refs page always visible (read-only for unauthenticated)
- Write access required for create/delete; default branch delete is blocked (button hidden)

**API endpoints** (all require `authMW`):

| Method | Path                                        | Description                                |
| ------ | ------------------------------------------- | ------------------------------------------ |
| POST   | `/api/repos/{owner}/{repo}/branches`        | Create branch (`name`, `from` form fields) |
| DELETE | `/api/repos/{owner}/{repo}/branches?name=…` | Delete branch                              |
| POST   | `/api/repos/{owner}/{repo}/tags`            | Create tag (`name`, `from` form fields)    |
| DELETE | `/api/repos/{owner}/{repo}/tags?name=…`     | Delete tag                                 |

HTMX responses swap `fragment-branches-list` into `#branches-list` and `fragment-tags-list` into `#tags-list`.

The ref badge on tree and commits pages links to `/{owner}/{repo}/refs` (via `RefsURL` field on `TreeData` / `CommitsData`).
