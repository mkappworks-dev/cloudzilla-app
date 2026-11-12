# Edit preview

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: —

| Branch | Worktree | PR title | Effort | Score gain |
| --- | --- | --- | --- | --- |
| `feat/edit-preview` | `.worktrees/feat+edit-preview` | `feat(repo): preview file edits as a diff` | M | Features +0.1, Interaction +0.1 |

Spec: [spec.md](../spec.md#edit-preview).

## Problem

The edit form is a bare textarea; the user can't see what they are about to commit.

## Scope

- Edit / Preview changes tabs on `edit_file.templ`; the textarea stays in the DOM across tabs.
- New endpoint `POST /{owner}/{repo}/edit-preview/{ref}/*` (htmx, `CanWrite`, same refusals as the edit page) returning
  a diff fragment of the opened file against the submitted content, with a rename line when the path changed. CSRF and
  the 1 MiB body cap apply as for `SubmitEditFile`.
- Extract the diff rendering used by commit and pull request pages into one fragment under `internal/view/fragments/`
  that takes `service.FileDiff`; ticket 02 reuses it. Add a `CodeService` method that diffs two byte slices for a path.
- Identical content and path shows "No changes to commit." and the same message the submit would give.
- Accessible tabs: `role="tablist"`, arrow keys, `aria-selected`, per the widget patterns from ticket 25.

## Files

- `internal/view/pages/edit_file.templ`, `internal/view/fragments/` (diff fragment), `internal/view/viewmodels_repo.go`
- `internal/handler/repo_files_handler.go`, `repo_files_page_handler.go`, `internal/router/router.go`
- `internal/service/code_service_files.go`; `docs/code-browser.md` (edit page section)

## Acceptance criteria

- [x] Preview shows a unified diff of the pending edit, including a path rename
- [x] Switching tabs keeps the typed content, path and message
- [x] Unchanged content shows "No changes to commit."
- [x] The existing commit, commit page and PR diffs render as before through the shared fragment (the commit page uses
      `fragments.FileDiff`; the PR files page keeps its own table for line comments and split view, sharing only
      `DiffCode`)
- [x] Handler tests: preview refusals (403, 404, 422), diff content, large body
- [x] `docs/code-browser.md` describes the preview

## Mock updates

`mockups/edit_file.html`: tabs, preview state with a diff, "No changes" state; `COVERAGE.md` if a state is not drawn.
Light and dark at 1440 under `make mockups`.

## Verification

Throwaway server per the harness in the [uiux spec](../../2026-11-06-uiux-desktop-9.5/spec.md), as `admin@example.test`
on `siteadmin/rusty-meadow`: edit a file, open Preview, rename, clear changes. Light and dark at 1440, evidence in the PR.
