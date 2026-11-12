# Heading, timestamp and release-excerpt consistency

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `tech/ui-consistency-sweep` | `.worktrees/tech+ui-consistency-sweep` | `tech(ui): heading, timestamp and release-excerpt consistency` | polish (wave 1 re-score) | M | Visual +0.2, Content +0.2 |

## Problem

Smaller findings both reviewers raised:

- Equivalent task pages use different heading hierarchies: file edit a 20px medium heading, issue creation the larger
  page heading (`edit_file.templ:18`, `gist_detail.templ:30`).
- Release summaries flatten headings and bullet lists into one long sentence (`releases.templ:73`).
- Timestamps mix "3 minutes ago", "3m ago", "Nov 6, 2026" and a raw "03 Nov 26 17:55 +0100" on the repo header
  (Claude; find the formatter that emits the last one).
- (Claude, unverified) Whether unknown routes render a styled 404 and server errors a styled 500; only `forbidden.templ`
  was found.

## Scope

One pass per bullet. The timestamp rule should live in one helper that all pages call.

## Files

- `internal/view/pages/edit_file.templ`, `gist_detail.templ`, `releases.templ`, `projects.templ`
- the time-formatting helpers in `internal/view/`
- matching mocks

## Acceptance criteria

- [x] One task-page heading style
- [x] Release excerpts keep paragraph breaks or show a plain-text first paragraph
- [x] ~~Project delete moves into a secondary control~~ (moved to ticket 36)
- [x] One relative/absolute timestamp rule across list and detail pages
- [x] 404 and 500 pages checked and styled

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Run after the wave 2 re-score (22).

2026-11-12 (wave 2 re-score): Wave 2 re-score: (Claude, Codex) release notes are flattened into one paragraph on the releases list (`repo-releases`, `releases-other`), the bullet above; (Claude) primary buttons mix green and black for equivalent actions (New issue, Draft release, New milestone, Comment against Save changes, Add file, discussion Comment), pick one token; three date formats on one page (`05 Oct 26 11:55 +0200`, `1mo`, `Oct 5, 2026`) and UTC on the commit page against raw offsets on tree and blob; Pre-release on the repo home but Latest on the releases list; (Codex) secondary metadata and section labels are very small and low in contrast on home, notifications and settings; (Claude) light-mode role text (Owner, Collaborator), 0% milestone tracks and the empty wiki ("This page does not exist yet." reads as an error; offer to create the Home page and hide Reorder with one page). Project delete (the destructive-row bullet) moves to ticket 36.

2026-11-12: the project-row Delete moved to ticket 36.

2026-11-12 (done): task pages use `text-2xl font-semibold` (file edit and add, gist detail). `internal/view/timefmt` is the one
timestamp helper: `Relative` ("3 minutes ago", an absolute date after a week), `Date`, `DateTime` (UTC) and `MonthYear`; the
raw `03 Nov 26 17:55 +0100` was `time.RFC822` in `tree.templ`, `repo.templ` and `blob.templ`. Create and save buttons use
`ButtonDefault`; `ButtonSuccess` stays for approve, reopen and restore. Release excerpts keep one line per heading, paragraph
or list item (`markdown.ExcerptLines`). Latest is never shown on a pre-release. 404 was already styled; the 500 was plain
text, now a styled page from `h.ServerError` and a panic `Recoverer`. Wave 2 items also done: 12px secondary metadata on home,
notifications and settings, light-mode role colours, visible 0% milestone tracks, empty wiki copy (Reorder hidden with one
page).
