# Edit conflict recovery, edit preview, new-branch pull requests and release assets

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent

Specs ticket 27 of [the desktop UI/UX effort](../2026-11-06-uiux-desktop-9.5/issues/27-edit-and-release-workflows.md).
Every UI ticket here follows that effort's [rules and verification harness](../2026-11-06-uiux-desktop-9.5/spec.md).

## Problem

1. A file edit that hits a conflict says "copy your changes, open the current version and edit that"
   (`internal/view/pages/edit_file.templ:28`). The user's text is kept, but nothing shows what changed upstream, and
   merging is left to them.
2. The edit form is a bare textarea that commits straight to the displayed branch: no diff of what will be committed,
   and no way to propose the change instead of committing it. On a branch whose rule has `require_pull_request` the
   submit is refused with 422 and the user has to leave the page (`docs/code-browser.md`, Protected branches).
3. The "New release" form describes releases as packaging downloads, but a release can't carry an uploaded asset
   (`release_new.templ`, `release_detail.templ`: the Assets card lists only the source zip).

## Decisions

Settled with the maintainer on 2026-11-12.

### Edit preview

- The edit page gets **Edit / Preview changes** tabs. Preview posts the form's current path and content to a new
  endpoint with htmx and shows a unified diff of the opened file against the text, plus a rename line when the path
  changed. The textarea stays in the DOM, so switching tabs loses nothing.
- The diff reuses the commit and pull-request diff renderer (`FileDiff` / `DiffHunk` in `code_service_commit.go`), put
  behind one fragment that ticket 02 reuses for the upstream diff.

### Conflict recovery

- On a stale save (`ErrFileChanged`, or the file deleted or moved on the branch) the page re-renders at 409 with the
  user's text, path and message kept and the form's `blob_sha` moved to the current blob.
- A **three-way line merge** runs with the blob the user opened as base, their text as "ours" and the current file as
  "theirs". If the changes don't overlap, the merged text goes into the textarea with a notice that upstream changes
  were merged. If they overlap, the textarea gets conflict blocks labelled `<<<<<<< your changes` and
  `>>>>>>> <branch> (current)`, and the notice says how many conflicts remain.
- The upstream diff (opened blob → current blob) is always shown beside the form, so even a clean merge is visible
  before it is committed.
- The commit is refused (422, same page) while any of the merge's own label lines remain in the text. Only the exact
  labels count, so a file that legitimately contains `=======` still commits.
- The merge is new code: go-git has no text merge. It lives in its own package with table tests. A binary or non-UTF-8
  current file skips the merge and shows only the diff notice.
- The conflict flow shares ticket 11's keep-the-input mechanism and ticket 33's inline error pattern
  (`components.FieldError`, `components.Invalid`, `static/field-validation.js`) for the marker error.

### Commit to a new branch and open a pull request

- The commit section becomes a radio choice: **Commit directly to `<branch>`** or **Create a new branch for this commit
  and open a pull request**, with an editable prefilled branch name (`<username>-patch-N`).
- The direct choice is disabled, with the reason, when the branch has `require_pull_request`, and the new-branch choice
  is preselected. The 422 refusal for protected branches stays as the server-side backstop.
- The new branch starts from the current tip of the edited branch, so the commit always sits on the latest history.
  The stale-save check and the conflict flow apply to both choices.
- A prefilled name that exists gets a counter (`-patch-2`); a name the user typed that exists is refused inline with
  422. The check and the commit happen in `CodeService`, which creates the ref and commits in one operation.
- After the commit the user is redirected to the existing pull request form
  (`/{owner}/{repo}/pulls/new?head=<new>&base=<edited branch>`), prefilled. The PR is not created for them.
- The push runs `PushService.AfterWebCommit` like any web commit. Editing a tag or commit SHA stays unavailable.

### Release assets

- `POST /api/repos/{owner}/{repo}/releases/{id}/assets` takes one file per request and streams it to the storage
  `Backend`. `DELETE .../assets/{assetID}` removes one. Both need `CanWrite` and work on draft and published releases.
- Limits: 512 MiB per file and 20 files per release, configurable (`release.max_asset_bytes`, `release.max_assets`,
  documented in `docs/configuration.md`). No type restriction. A repeated name in one release is 409.
- Names are cleaned to `[A-Za-z0-9._-]`, at most 100 characters. Storage keys are
  `releases/repo/<repo id>/<release id>/<token>.bin`; the user's filename never reaches the key.
- `GET /{owner}/{repo}/releases/download/{tag}/{name}` serves an asset, with the permission check on every request (a
  draft's assets for writers only, a private repo's for readers only; anything else is the same 404). Responses are
  `application/octet-stream` with `Content-Disposition: attachment`, `nosniff` and the sandbox CSP, as for
  attachments. A tag containing `/` stays in one segment, as `/releases/tag/{tagName}` does.
- Each asset records name, size, SHA-256 (computed while streaming), uploader and upload time. The detail page lists
  them with the source zip; writers get a delete control and an "Add asset" control. No download counts, no webhook, no
  notification.
- Deleting a release or its repo deletes the objects after the database change commits; a failed object delete is
  logged and left as an orphan, as in `docs/storage.md`.
- The new-release form lets the user pick files. After the release is created (draft or published) the browser uploads
  them one at a time with per-file progress, then goes to the detail page. A failed upload leaves the release in place,
  and the detail page offers a retry through "Add asset".

## Out of scope

Editing tags or commits through the web, asset replace, download counts, resumable or chunked uploads, signed asset
URLs, and a visual conflict resolver beyond the textarea with markers.

## Tickets

| #   | Ticket                                              | Blocked by | Shares files                                   |
| --- | --------------------------------------------------- | ---------- | ---------------------------------------------- |
| 01  | [Edit preview](./issues/01-edit-preview.md)         | —          | `edit_file.templ`, `repo_files_handler.go`     |
| 02  | [Edit conflict recovery](./issues/02-edit-conflict-recovery.md) | 01 | `edit_file.templ`, `repo_files_handler.go` |
| 03  | [Commit to a new branch and open a pull request](./issues/03-edit-new-branch-pull-request.md) | 02 | `edit_file.templ`, `repo_files_handler.go` |
| 04  | [Release assets: storage, API, download, detail list](./issues/04-release-assets.md) | — | `release_detail.templ`, `release_handler.go` |
| 05  | [New-release form uploads](./issues/05-release-new-uploads.md) | 04 | `release_new.templ` |

Parallel-safe: the edit chain (01 → 02 → 03) and the release chain (04 → 05) touch no common file, so the two chains can
run as two sessions at once. Inside a chain the tickets are serial because they edit the same template and handler.
