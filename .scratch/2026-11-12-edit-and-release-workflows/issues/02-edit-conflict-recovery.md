# Edit conflict recovery

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: 01

| Branch | Worktree | PR title | Effort | Score gain |
| --- | --- | --- | --- | --- |
| `feat/edit-conflict-recovery` | `.worktrees/feat+edit-conflict-recovery` | `feat(repo): recover from file edit conflicts with a three-way merge` | L | Edge +0.2 |

Spec: [spec.md](../spec.md#conflict-recovery).

## Problem

A stale save tells the user to copy their text, reopen the file and start over; nothing shows the upstream change.

## Scope

- On `ErrFileChanged` (and a file deleted or moved on the branch) re-render at 409 with the user's text, path and
  message kept, `blob_sha` moved to the current blob, and the upstream diff (opened blob → current blob) shown through
  ticket 01's diff fragment. A file deleted upstream shows a message and the user's text, with no merge.
- New package (for example `internal/textmerge`) with a three-way line merge: base = opened blob, ours = user text,
  theirs = current file. Clean merge: merged text in the textarea with a notice. Overlap: conflict blocks labelled
  `<<<<<<< your changes` / `=======` / `>>>>>>> <branch> (current)` and a notice with the conflict count. Binary or
  non-UTF-8 current content skips the merge.
- `SubmitEditFile` refuses (422, same page) while the merge's label lines remain; only the exact labels count. The error
  uses `components.FieldError` / `Invalid` on the contents field and moves focus there (ticket 33 pattern).
- Line endings follow `textareaText` / `allCRLF`; the merged text must not change a file's line-ending convention.
- Remove the "open the current version" copy and `ConflictURL` use that it replaces.

## Files

- `internal/handler/repo_files_handler.go`, `internal/view/pages/edit_file.templ`, `internal/view/viewmodels_repo.go`
- new `internal/textmerge/` with tests; `docs/code-browser.md` (Stale saves, Edit page)

## Acceptance criteria

- [ ] A non-overlapping upstream change merges into the textarea, with a notice and the upstream diff beside it
- [ ] An overlapping change shows conflict blocks, a conflict count and the upstream diff; the user's text is kept
- [ ] Committing with the merge's markers still present is refused inline; a file containing `=======` still commits
- [ ] Resubmitting the resolved text commits on the current tip
- [ ] Table tests for the merge (identical, adjacent edits, overlap, one-sided delete, CRLF, no trailing newline) and
      handler tests for the conflict re-render
- [ ] Docs updated

## Mock updates

`mockups/edit_file.html`: clean-merge state, conflict-markers state with the upstream diff, marker-refused state.
Light and dark at 1440.

## Verification

Throwaway server per the harness: open an edit, commit to the same file from a second session or `git push`, submit the
first edit. Check clean-merge, overlap and deleted-upstream cases, light and dark at 1440.
