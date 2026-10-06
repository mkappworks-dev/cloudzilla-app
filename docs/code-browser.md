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
| Edit file (GET/POST)    | `/{owner}/{repo}/edit/{branch}/{path...}` |
| Delete file (POST)      | `/{owner}/{repo}/delete/{branch}/{path...}` |

`{ref}` = branch name, tag name, or commit SHA. Pagination via `?page=N` (1-indexed, 30 per page).

A ref may contain `/` (`feature/x`), so chi's one-segment `{ref}` can't tell where it ends. Handlers rejoin `{ref}` and `*` (`routeRefPath`) and split them with `CodeService.SplitRefPath`: the longest branch or tag that ends at a segment boundary wins, and otherwise the first segment is the ref. The new-file POST and archive routes take the whole tail as the ref. Raw goes through `SplitRefPath` too, so `/raw/feature/x/README.md` serves `README.md` on `feature/x`.

Git allows `#`, `?`, `%` and `'` in refs and file names, so every link and redirect to a ref or path is built with `internal/codeurl`: `codeurl.Path(owner, repo, kind, ref, path)` escapes each `/`-separated segment of the ref and path with `url.PathEscape`, keeping the slashes as separators. Results, `data-path` and the `cz_tree_open` cookie keep the decoded values; only URLs carry the escaped form. Inbound, chi routes on `r.URL.RawPath` whenever Go sets it, which happens when the request's escaping differs from Go's (a browser leaves `'` unescaped, Go escapes it), and only then are `{ref}` and `*` still escaped. `routeRefPath` therefore decodes the params only when `RawPath` is set; for `pct%2541` chi already hands back `pct%41`, which a second decode would turn into `pctA`. chi isn't switched to route on the decoded path, because `middleware/scope.go` splits the same `RawPath` so an encoded `%2F` can't shift segments between the router and the token-target policy, and `/releases/tag/{tagName}` relies on a tag's `%2F` staying in one segment: release links `url.PathEscape` the whole tag, and `PageReleaseDetail` decodes it the same way. The refs page's delete buttons `url.QueryEscape` the name they send.

A tree URL whose path is a file redirects (302) to its blob URL. Tree and blob pages share the file tree sidebar (`components.FileTreeSidebar`), built by `buildSidebarTree` from the requested ref and path. Folders the viewer opens stay open: `static/file_tree.js` keeps them in the per-repo session cookie `cz_tree_open`, which the server reads to render them open. A folder rendered closed loads its children once from `/fragments/{owner}/{repo}/tree/{ref}/{path}`. The filter shows each loaded item whose name matches along with the folders above it, deriving that from the DOM without touching `open` or the cookie.

---

## CodeService (`internal/service/code_service.go`)

`CodeService` has **no store dependency** — it reads git data directly from bare repos on disk via go-git. It is wired in `services.New()` and receives `config.GitConfig` (for `ReposRoot`).

### Key Methods

- `ResolveRef(owner, repoName, ref)` → `(*object.Commit, ref, error)` — the returned ref is the branch or tag name, the full SHA, or HEAD's branch
- `GetTree(owner, repoName, ref, path)` → `*TreeResult`
- `ListEntriesWithLastCommit(ctx, repo, ref, dir)` → `[]TreeEntryWithLastCommit` — each entry with the last commit that touched it; cached for 60s per repo ID, resolved commit and dir, so a new commit, or a new repo that takes a deleted or transferred repo's name, is always listed fresh
- `GetBlob(owner, repoName, ref, path)` → `*BlobResult`
- `OpenRawBlob(owner, repoName, ref, path, maxBytes)` → `*RawBlob` — the file open for streaming, with `Size` and `IsBinary`; `ErrBlobTooLarge` past `maxBytes`. The caller closes it
- `GetBlame(owner, repoName, ref, path)` → `*BlameResult`
- `GetCommits(owner, repoName, ref, page, pageSize)` → `*CommitLog`
- `GetCommit(owner, repoName, sha)` → `*CommitDetail`
- `HighlightDiffs(owner, repoName, files)` — fills `DiffLine.HTML` in place; see [Syntax highlighting](#syntax-highlighting)
- `ListRefs(owner, repoName, defaultBranch)` → `*RefsResult` — an annotated tag's `Hash` is its tag object's
- `ListRefsPeeled(owner, repoName, defaultBranch)` → `*RefsResult` — tags' `Hash` peeled to the tagged commit, at an object read per tag; for the refs page, which shows the hashes
- `SplitRefPath(owner, repoName, refPath)` → `(ref, path)`
- `GetBranchFile(owner, repoName, branch, path, maxContent)` → `*BranchFile` — the file on a branch's tip (any other ref is `ErrRefNotFound`) with its SHA and mode, and its content when it's text within `maxContent`
- `EditFile(owner, repoName, branch, oldPath, newPath, baseSHA, content, author, message)` → `error`; `DeleteFile(owner, repoName, branch, path, baseSHA, author, message)` → `(remainingDir, error)` — see [Editing and Deleting Files](#editing-and-deleting-files)
- `CreateBranch(owner, repoName, name, fromRef)` → `error`
- `DeleteBranch(owner, repoName, name)` → `error`
- `CreateTag(owner, repoName, name, fromRef)` → `error`
- `DeleteTag(owner, repoName, name)` → `error`

### ResolveRef Priority

1. Branch: `repo.Reference(plumbing.NewBranchReferenceName(ref), true)`
2. Tag: `repo.Reference(plumbing.NewTagReferenceName(ref), true)`, peeled through annotated tags (and tags of tags) by `peelTag`; a tag of a tree or blob is not found
3. Raw SHA: `repo.CommitObject(plumbing.NewHash(ref))`
4. HEAD fallback (when `ref == ""`): `repo.Head()`

Returns `ErrEmptyRepo` sentinel when HEAD resolution fails (repo has no commits). Handlers return 404 on this error.

A raw SHA must be the full 40 characters; an abbreviated one doesn't resolve. The `Ref` on result types is therefore the full SHA, and every URL is built from it. Only displayed text shortens it, through `components.RefLabel`.

### Result Types

- `TreeResult` — `Entries []TreeEntry` (dirs first, then files, both sorted), `Ref`, `Path`, `Breadcrumbs`
- `BlobResult` — `Lines []CodeLine`, `IsBinary bool`, `SHA`, `IsSymlink`, `IsBranch` (the ref named a branch), `BlameURL`, breadcrumbs
- `BlameResult` — `Lines []BlameLine` with `ShowMeta bool` (true when commit run changes), `BlobURL`, breadcrumbs
- `CommitLog` — `Commits []CommitSummary`, `Ref`, `Page`, `PrevPage`, `NextPage`, `HasMore`
- `CommitDetail` — full commit with `Files []FileDiff` (hunks with add/del/ctx lines), `TotalAdded`, `TotalDeleted`
- `RefsResult` — `Branches []BranchInfo` (`Name`, `Hash`, `IsDefault`), `Tags []TagInfo` (`Name`, `Hash`)

---

## Editing and Deleting Files

Writers edit, rename and delete files from the browser; each is one commit onto the branch, authored as New file's commits are (`UserService.CommitAuthor`, so the keep-email-private setting applies).

- **Where:** the blob page shows Edit for a text file of at most 1 MiB and Delete for any file or symlink, only on a branch (not a tag or commit SHA), to a viewer with `CanWrite`, on a repo that isn't archived. The routes refuse the same cases: 404 for an unreadable repo, a ref that isn't a branch, or a path that isn't a file; 403 for a non-writer or an archived repo; 422 when the editor can't open the file (binary, symlink, over 1 MiB).
- **Stale saves:** the edit page and the delete dialog carry the blob SHA they loaded. `CodeService.EditFile` and `DeleteFile` refuse with `ErrFileChanged` (409) unless the path on the branch still holds that blob, and otherwise land on the current tip, so a commit to another file doesn't block them. `gitref.Move` still turns a push mid-write into `ErrRefMoved` (409). Neither creates a branch, which `CommitFile` does for an empty repo.
- **Edit page** (`PageEditFile` / `SubmitEditFile`): changing the path renames the file, keeping its mode, creating folders and pruning the ones it empties; a target that holds anything is `ErrPathCollision` (409). An edit that changes neither path nor content is `ErrFileUnchanged` (422). A refusal re-renders the page at its status with the user's path, content and message kept. The textarea renders `"\n" + content` because the HTML parser drops a newline right after `<textarea>`.
- **Line endings:** a browser submits textarea line breaks as CRLF. `textareaText` turns them back into LF, or keeps CRLF when the original file used CRLF on every line. New file's textarea goes through it too; uploads are committed byte for byte. Content over 1 MiB after that is refused with 413, and the route's body cap (`MaxEditFileBodyBytes`) leaves room for CRLF doubling.
- **Delete dialog:** posts with htmx to `DeleteFile`, which prunes the folders the deletion empties and answers `HX-Redirect` to the nearest folder left (or the branch's root tree). Refusals come back as JSON `{"error"}`, which `ConfirmDialog` shows as a toast.
- Web commits fire no push side effects (webhooks, activity, stats, search index) and consult no branch protection; `block_force_push` can't trigger on a fast-forward.

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

## Syntax highlighting

`internal/highlight` runs Chroma on the server and emits `<span class="hl-<token>">` with no inline colors. `cmd/server/frontend/static/code-themes.css` holds every catalog theme, scoped by `html.dark[data-code-dark="…"]` or `html:not(.dark)[data-code-light="…"]`. The layout writes the viewer's saved pair (Settings → Appearance, `users.code_theme_light/dark`) onto `<html>`, so the site's light/dark toggle switches code themes with no script. Code containers carry the `hl` class for the theme's background.

- **Where:** `GetBlob` and `GetBlame` fill `CodeLine.HTML`/`BlameLine.HTML`; the gist page and `internal/markdown` (fenced blocks in a known language) call `highlight.BlockWithin` against a per-page or per-document `Budget`. Diffs are highlighted only when a page calls `CodeService.HighlightDiffs`: `GetCommit` and `GetPullDiff` also serve post-receive stats and CODEOWNERS, which must not pay for it.
- **Diffs** highlight both sides' whole blobs and map lines by number, because a hunk can start inside a block comment. A line gets HTML only when the blob's line equals `DiffLine.Content`; anything else renders plain.
- **Caps:** sources over 512 KiB, calls over 500 ms, and diff pages past 4 MiB / 2 s render plain. Markdown highlighting is capped at 256 KiB / 250 ms per rendered document and, on top, 1 MiB / 1 s per HTTP request, and a gist page at 1 MiB / 1 s; fences or files past a cap render plain. The request budget is seeded by `middleware.HighlightBudget` and carried on the context (`highlight.WithBudget` / `highlight.BudgetFrom`); handlers render with `markdown.RenderCtx(r.Context(), src)`, which charges each fence to both its document's budget (`Budget.Sub`) and the request's, so an issue, PR or discussion with many comments can't multiply the per-document cap. Its clock starts when the request does, and those pages render the top body before the comments so it is first in line. `Budget` is safe for concurrent use; `markdown.Render` (no context) keeps only the per-document cap. The deadline is checked between tokens, and a call also gives up after 1,000 empty tokens in a row (Jungle and JSONata emit them forever on input like `{`). Lexers are picked by filename, plus shell shebang sniffing for extensionless scripts.
- **Always plain:** Svelte, ERB, PHTML, YAML+Jinja and Go HTML Template, including as Markdown fences or other nested blocks. chroma's delegating lexers tokenise the whole input before returning the first token, so no deadline can stop them (a 256 KiB Svelte file took 4 s). At init, `highlight` replaces every delegating lexer in chroma's global registry with a plain stand-in, because nested lookups (`Using`, `UsingByGroup`, `lexers.Get`) resolve through that registry too. `TestLexers_EveryRegisteredLexerReturnsPromptly` runs every registered lexer over adversarial input; swap in a stand-in for any lexer it catches.
- **Themes:** the catalog is `highlight.Themes`. After changing it, run `make generate-code-themes`; a test fails while the committed CSS is stale. Stored IDs outside the catalog read back as the defaults (`github` / `github-dark`). `highlight.PlainTheme` (`plain`) is offered in both modes and has no CSS rules, so code keeps the site's own text and background colours. Settings → Appearance picks themes with `components.SelectMenu`, showing `highlight.Swatch` colours beside each name.
