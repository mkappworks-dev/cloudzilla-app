# Code Browser & Refs

## URL Patterns

| View                    | Route                                     |
| ----------------------- | ----------------------------------------- |
| Root tree               | `/{owner}/{repo}/tree/{ref}`              |
| Subtree                 | `/{owner}/{repo}/tree/{ref}/{path...}`    |
| Blob                    | `/{owner}/{repo}/blob/{ref}/{path...}`    |
| Blame                   | `/{owner}/{repo}/blame/{ref}/{path...}`   |
| Raw file                | `/{owner}/{repo}/raw/{ref}/{path...}`     |
| Commit log              | `/{owner}/{repo}/commits/{ref}`           |
| Commit log (file scope) | `/{owner}/{repo}/commits/{ref}/{path...}` |
| Single commit diff      | `/{owner}/{repo}/commit/{sha}`            |
| Branches & Tags         | `/{owner}/{repo}/refs`                    |

`{ref}` = branch name, tag name, or commit SHA. Pagination via `?page=N` (1-indexed, 30 per page).

A ref may contain `/` (`feature/x`), so chi's one-segment `{ref}` can't tell where it ends. Handlers rejoin `{ref}` and `*` (`routeRefPath`) and split them with `CodeService.SplitRefPath`: the longest branch or tag that ends at a segment boundary wins, and otherwise the first segment is the ref. The new-file POST and archive routes take the whole tail as the ref.

A tree URL whose path is a file redirects (302) to its blob URL. Tree and blob pages share the file tree sidebar (`components.FileTreeSidebar`), built by `buildSidebarTree` from the requested ref and path. Folders the viewer opens stay open: `static/file_tree.js` keeps them in the per-repo session cookie `cz_tree_open`, which the server reads to render them open. A folder rendered closed loads its children once from `/fragments/{owner}/{repo}/tree/{ref}/{path}`.

---

## CodeService (`internal/service/code_service.go`)

`CodeService` has **no store dependency** — it reads git data directly from bare repos on disk via go-git. It is wired in `services.New()` and receives `config.GitConfig` (for `ReposRoot`).

### Key Methods

- `ResolveRef(owner, repoName, ref)` → `(*object.Commit, displayRef, error)`
- `GetTree(owner, repoName, ref, path)` → `*TreeResult`
- `ListEntriesWithLastCommit(ctx, repo, ref, dir)` → `[]TreeEntryWithLastCommit` — each entry with the last commit that touched it; cached for 60s per repo ID, resolved commit and dir, so a new commit, or a new repo that takes a deleted or transferred repo's name, is always listed fresh
- `GetBlob(owner, repoName, ref, path)` → `*BlobResult`
- `OpenRawBlob(owner, repoName, ref, path, maxBytes)` → `*RawBlob` — the file open for streaming, with `Size` and `IsBinary`; `ErrBlobTooLarge` past `maxBytes`. The caller closes it
- `GetBlame(owner, repoName, ref, path)` → `*BlameResult`
- `GetCommits(owner, repoName, ref, page, pageSize)` → `*CommitLog`
- `GetCommit(owner, repoName, sha)` → `*CommitDetail`
- `ListRefs(owner, repoName, defaultBranch)` → `*RefsResult`
- `SplitRefPath(owner, repoName, refPath)` → `(ref, path)`
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

## Raw Files

`RawFile` streams a file through `OpenRawBlob`. A viewer who can't read the repo gets the same 404 as for a missing repo. A file is served as `text/plain; charset=utf-8`, or as `application/octet-stream` when the blob page would call it binary (a NUL byte in the first 8000). Every raw response carries `X-Content-Type-Options: nosniff` and `Content-Security-Policy: default-src 'none'; sandbox`, so an uploaded `.html` or `.svg` can't run script on the forge's origin. Files over 25 MB get a 403 telling the user to clone instead, because go-git inflates a packed blob whole before the first byte can be read.

---

## Branch & Tag Management

The Refs page (`/{owner}/{repo}/refs`) lists all branches and tags. Authenticated users with write access can create and delete branches/tags via HTMX forms.

**Permission rules:**

- Public repos: refs page always visible (read-only for unauthenticated)
- Write access required for create/delete; default branch delete is blocked (button hidden)
- Deleting a branch whose protection rule has `block_force_push` is refused with 422; the button stays, and the refusal shows as an error toast

**API endpoints** (all require `authMW`):

| Method | Path                                        | Description                                |
| ------ | ------------------------------------------- | ------------------------------------------ |
| POST   | `/api/repos/{owner}/{repo}/branches`        | Create branch (`name`, `from` form fields) |
| DELETE | `/api/repos/{owner}/{repo}/branches?name=…` | Delete branch                              |
| POST   | `/api/repos/{owner}/{repo}/tags`            | Create tag (`name`, `from` form fields)    |
| DELETE | `/api/repos/{owner}/{repo}/tags?name=…`     | Delete tag                                 |

HTMX responses swap `fragment-branches-list` into `#branches-list` and `fragment-tags-list` into `#tags-list`.

The repo home, tree, blob and blame pages share a branch/tag picker (`components.RefPicker`) that lists every ref from `ListRefs`. On tree, blob and blame, each item opens the same path on that ref, so a path missing there 404s; on the repo home, the default branch opens the home page and other refs open their tree. The commits page's ref badge links to `/{owner}/{repo}/refs` (via `RefsURL` on `CommitsData`).
