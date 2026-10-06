# Code-Browser Path Escaping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Code-browser URLs resolve when a path or ref contains characters whose browser encoding differs from Go's (`'`), and every link the forge builds to a ref or path survives `#`, `?` and `%`.

**Architecture:** `routeRefPath` decodes chi's `{ref}`/`*` params only when `r.URL.RawPath` is set, which is exactly when chi routed on the escaped form. A new leaf package `internal/codeurl` escapes each `/`-separated segment of refs and paths, and every URL builder in services, handlers and templates goes through it.

**Tech Stack:** Go, chi v5, go-git v5, Templ.

**Spec:** `docs/superpowers/specs/2026-10-06-code-browser-path-escaping-design.md`

## Global Constraints

- Work in `/home/user/cloudzilla-app` on branch `fix/code-browser-path-escaping`.
- Integration tests need `TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5433/cloudzilla_test?sslmode=disable'` (already migrated). Without it DB tests skip silently, so always set it.
- Edit `.templ` files, then run `make generate-templ` (`~/go/bin/templ` is installed). Never hand-edit `*_templ.go`. Don't run `templ fmt`.
- Comments only for a *why* the code can't show, one line by default.
- Commit with `git add <specific paths>`. Never stage `.claude/`. Conventional Commits subject, ending with the trailers:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01Qh8wB1QRx9ksNMBKhN1KQm`.
- Before each commit: `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, and the touched packages' tests with the DSN set.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/codeurl/codeurl.go` (new) | `Escape`, `Path` |
| `internal/codeurl/codeurl_test.go` (new) | unit tests |
| `internal/handler/page_repo_handler.go` | `routeRefPath` decode; redirects and `RawURL` |
| `internal/handler/file_tree_handler.go` | sidebar `Href`/`ChildrenURL`, `expandAllURL` |
| `internal/handler/repo_files_handler.go` | post-commit redirect |
| `internal/service/code_service.go`, `code_service_tree.go`, `code_service_blame.go` | breadcrumbs, entry URLs, `BlameURL`, `BlobURL` |
| `internal/view/pages/helpers.go` and the templates listed in the spec | link building |
| `internal/handler/page_code_browser_test.go` | router-level tests |
| `docs/code-browser.md` | note on decoding and `codeurl` |

---

## Task 1: `internal/codeurl`

- [ ] Write `codeurl_test.go`: `Escape` cases `""`→`""`, `a/b`→`a/b`, `odd "q'uote"/note.txt`→`odd%20%22q%27uote%22/note.txt`, `hash#q?`→`hash%23q%3F`, `pct%41`→`pct%2541`, `feature/x`→`feature/x`; `Path("o","r","blob","feature/a#b","d/f.txt")`→`/o/r/blob/feature/a%23b/d/f.txt`; empty path gives no trailing slash. Run it and watch it fail.
- [ ] Implement:
  ```go
  // Package codeurl builds code-browser URLs from git refs and repo paths.
  package codeurl

  func Escape(p string) string // url.PathEscape per "/"-separated segment
  func Path(owner, repo, kind, ref, path string) string
  ```
- [ ] Tests pass. Commit: `fix(code): add codeurl to escape refs and paths in URLs`.

## Task 2: decode `{ref}/*` params (inbound)

- [ ] In `page_code_browser_test.go`, add a fixture (extend `seedCodeRepo` or add a sibling seeding helper) that also commits `odd "q'uote"/note.txt`, `hash#q?/n.txt` and `pct%41/n.txt` to `main`.
- [ ] Table test, 200 expected for each of: `/tree/main/odd%20%22q'uote%22`, `/blob/main/odd%20%22q'uote%22/note.txt`, `/blame/…/note.txt`, `/raw/…/note.txt`, `/fragments/{owner}/{repo}/tree/main/odd%20%22q'uote%22`, the same five fully escaped (`%27`), `/tree/main/hash%23q%3F`, and `/tree/main/pct%2541` (regression: no double decode). Run it: the unescaped-`'` rows must fail with 404.
- [ ] In `routeRefPath`, when `r.URL.RawPath != ""`, return `url.PathUnescape` of the rejoined value, and the raw value if that errors. Add a one-line why comment: chi routes on `RawPath` when set, so its params are still escaped only then.
- [ ] Tests pass. Commit: `fix(code): decode ref paths chi routed on the raw path`.

## Task 3: service and handler URL builders

- [ ] Tests first: for the `hash#q?` fixture, the root tree page contains `href="/{o}/{r}/tree/main/hash%23q%3F"`; the blob page for `hash#q?/n.txt` has breadcrumbs, `RawURL` and blame link escaped, and its sidebar `Href`/`hx-get` escaped; `GET /tree/main/hash%23q%3F/n.txt` 302s to the escaped blob URL. Each rendered href, requested, returns 200. Watch them fail.
- [ ] Switch every site in the spec's Service and Handlers lists to `codeurl.Path` / `codeurl.Escape`. `expandAllURL` keeps its `?expand=all&active=` query.
- [ ] Update any existing test that asserted an unescaped URL only if it involved characters that now escape (none expected). Commit: `fix(code): escape refs and paths in code-browser links and redirects`.

## Task 4: templates

- [ ] Test first: create branch `a#b` from `main`; the refs page links its tree as `/tree/a%23b` and commits as `/commits/a%23b`, and `GET /{o}/{r}/tree/a%23b` returns 200. Watch it fail.
- [ ] Route every template site in the spec through `codeurl` (`codeRefHref`, `repoHomeRefHref`, and the inline concatenations). Run `make generate-templ`, then grep the `_templ.go` output for `codeurl.` to confirm.
- [ ] `grep -rnE '(tree|blob|blame|raw|commits|new|archive)/" ?\+' --include=*.go --include=*.templ internal | grep -v _templ.go | grep -v _test.go` returns nothing.
- [ ] Commit: `fix(ui): escape refs and paths in repo, refs, PR and search links`.

## Task 5: docs

- [ ] In `docs/code-browser.md`, under URL Patterns, add: links escape each ref and path segment with `codeurl`; `routeRefPath` decodes only when `RawPath` is set, and why chi isn't switched to the decoded path (`scope.go`). Commit: `docs(code): describe code-browser URL escaping`.

## Review

A subagent reviews the full diff against the spec: missed builders, double encoding (`templ.SafeURL` doesn't escape, `templ.URL` would), and unescaped values leaking into `data-path` or the cookie.
