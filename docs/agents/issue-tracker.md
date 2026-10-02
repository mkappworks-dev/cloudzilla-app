# Issue tracker: Local Markdown

Issues and specs for this repo live as markdown files in `.scratch/`.

## Conventions

- One feature per directory: `.scratch/<feature-slug>/`
- The spec is `.scratch/<feature-slug>/spec.md`
- Implementation issues are one file per ticket at `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01`, never a single combined tickets file
- Near the top of each spec and ticket: `Created:` (`YYYY-MM-DD`), `Category:` (`bug` or `enhancement`, set by triage) and `Status:` (a role string from `triage-labels.md`, or `done`)
- Comments and conversation history append to the bottom of the file under a `## Comments` heading, each starting with its author and date
- Commit these files with the branch that writes them: every branch has its own worktree, so an uncommitted ticket is invisible to the others

## Lifecycle

- **Open**: any `Status:` except `done` and `wontfix`, or none yet (never triaged)
- **List**: `.scratch/*/spec.md` and `.scratch/*/issues/*.md`, oldest `Created:` first, skipping wayfinder efforts (folders with a `map.md`)
- **Done**: when a ticket's work is complete, tick its acceptance criteria and set `Status: done` in the same commit. A ticket is unblocked when everything in its `Blocked by` is `done`
- **Close**: set `Status: wontfix` and append the reason as a comment

## When a skill says "publish to the issue tracker"

Create a new file under `.scratch/<feature-slug>/` (creating the directory if needed).

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path. A bare ticket number is unique only within its feature folder, and `#N` (as in commit and PR titles) is a GitHub PR or issue, never a local ticket.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a file with one **child** file per ticket. These tickets use the `Type:` and `Status:` values below instead of the triage roles.

- **Map**: `.scratch/<effort>/map.md` (the Notes / Decisions-so-far / Fog body).
- **Child ticket**: `.scratch/<effort>/issues/NN-<slug>.md`, numbered from `01`, with the question in the body. A `Type:` line records the ticket type (`research`/`prototype`/`grilling`/`task`); a `Status:` line records `claimed`/`resolved`.
- **Blocking**: a `Blocked by: NN, NN` line near the top. A ticket is unblocked when every file it lists is `resolved`.
- **Frontier**: scan `.scratch/<effort>/issues/` for files that are open, unblocked, and unclaimed; first by number wins.
- **Claim**: set `Status: claimed` and save before any work.
- **Resolve**: append the answer under an `## Answer` heading, set `Status: resolved`, then append a context pointer (gist + link) to the map's Decisions-so-far in `map.md`.
