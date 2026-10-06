# 2026-10-06 — Code-browser path escaping

**Status:** Design approved.
**Branch:** `fix/code-browser-path-escaping`
**Affected subsystems:** `routeRefPath` (`internal/handler/page_repo_handler.go`), a new `internal/codeurl` package, `CodeService` URL fields and breadcrumbs (`internal/service/`), the file tree sidebar and code-browser redirects (`internal/handler/`), every Templ link that puts a ref or repo path into a URL, `docs/code-browser.md`.

---

## Background

A folder named `odd "q'uote"` holding `note.txt` 404s on its tree, blob, blame and raw pages and on its sidebar fragment.

Browsers leave `'` unescaped in paths, but Go's `url.URL.EscapedPath` escapes it. The two forms differ, so Go keeps the request's form in `r.URL.RawPath`. chi routes on `RawPath` whenever it is set. `{ref}` and `*` therefore come back still escaped (`odd%20%22q'uote%22`), and `GetTree`/`GetBlob` look up a path that doesn't exist. A path whose escaping matches Go's (`%27`, or no `'` at all) has an empty `RawPath`, and chi hands back decoded params. Verified through the real router: `…/blob/main/odd%20%22q'uote%22/note.txt` → 404, `…/blob/main/odd%20%22q%27uote%22/note.txt` → 200.

Separately, every link to a ref or path is built by concatenating raw names. A name containing `#`, `?` or `%` produces an href the browser cuts at the fragment or query, or decodes into another name. Git allows those characters in file names and in branch and tag names.

## Locked decisions

1. **Decode in `routeRefPath`, only when `r.URL.RawPath != ""`.** That is exactly when chi's params are still escaped. Decoding unconditionally would double-decode: `pct%2541` arrives as param `pct%41` with an empty `RawPath` and resolves today. If `url.PathUnescape` fails, the value is returned unchanged and 404s as before. Every `{ref}/*` route (tree, blob, blame, commits, file tree fragment, new file, upload, archive) already reads through `routeRefPath`. Raw read `{ref}` and `*` separately, so it 404'd on refs containing `/` as well; it now reads through `routeRefPath` and `SplitRefPath` like the rest.
2. **Don't make chi route on the decoded path.** `middleware/scope.go`'s `pathSegments` deliberately splits the same `RawPath` chi routes on, so an encoded `%2F` can't shift segments between the router and the token-target policy. `/releases/tag/{tagName}` also depends on a tag's `%2F` staying inside one segment, so release links `url.PathEscape` the whole tag and `PageReleaseDetail` decodes it when `RawPath` is set. Delete buttons on the refs page `url.QueryEscape` the name in `?name=`.
3. **New package `internal/codeurl`**, importable by service, handler and view:
   - `Escape(p string) string`: `url.PathEscape` on each `/`-separated segment. Slashes stay separators, so a ref like `feature/x` still splits through `SplitRefPath`.
   - `Path(owner, repo, kind, ref, path string) string`: `/{owner}/{repo}/{kind}/{Escape(ref)}`, plus `/{Escape(path)}` when `path != ""`.
4. **Escape refs as well as paths, in every link and redirect.** Owner and repo names are slugs and are left alone.
5. **The decoded value is the source of truth server-side.** `ref` and `path` stay unescaped in results (`TreeResult.Path`, `data-path`, the `cz_tree_open` cookie) and are escaped only when written into a URL.

## Call sites

Service (`internal/service/`):

- `code_service_tree.go`: entry `URL`, `BlameURL`
- `code_service_blame.go`: `BlobURL`
- `code_service.go`: `buildBreadcrumbs` tree/blob URLs

Handlers (`internal/handler/`):

- `file_tree_handler.go`: `buildSidebarLevel` `Href` and `ChildrenURL`; `expandAllURL`
- `page_repo_handler.go`: tree→blob redirect, `RawURL`, commits redirect to the default branch
- `repo_files_handler.go`: redirect after a new-file commit

Templates (`internal/view/`):

- `pages/helpers.go` `codeRefHref`; `pages/repo.templ` `repoHomeRefHref`, entry and README links, `/new/`, `/archive/…zip`, `/commits/` links
- `pages/tree.templ`, `pages/commits.templ`, `pages/commit.templ`, `pages/new_file.templ` form `action`
- `pages/refs.templ`, `fragments/refs.templ`
- `pages/pr_commits.templ`, `pages/pr_files.templ`, `fragments/pull_chrome.templ`
- `pages/code_search.templ`, `pages/release_detail.templ` archive link

A SHA-only link (`/tree/` + `c.FullHash`) is safe as is but goes through `codeurl` too, so one grep finds every builder.

## Tests

Written first, through the real router (`internal/handler/page_code_browser_test.go`):

- Fixture adds `odd "q'uote"/note.txt`, `hash#q?/n.txt` and `pct%41/n.txt`.
- Tree, blob, blame, raw and `/fragments/…/tree/…` return 200 for the browser's form (unescaped `'`) and for the fully escaped form.
- `pct%2541` still resolves to `pct%41` (no double decode).
- The tree listing, breadcrumbs and sidebar render escaped hrefs (`hash%23q%3F`), and requesting each one returns 200.
- A branch named `a#b` gets an escaped href on the refs page, and that href opens its tree.

Unit tests for `codeurl.Escape` and `codeurl.Path`.

## Out of scope

- Ref and path strings in the JSON API.
- Relative links inside rendered Markdown.
