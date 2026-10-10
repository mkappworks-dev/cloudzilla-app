# Projects: GitHub-style list, board and rich cards

Created: 2026-10-09
Category: enhancement
Status: done

## Problem

- `/{owner}/{repo}/projects`: only the title of a row is a link, and "New project" toggles an inline form.
- `/{owner}/{repo}/projects/{id}`: cards are not clickable as a whole, "+ Add card" uses `prompt()` and an `i5`/`p3` syntax, "+ Column" is a header button.
- A card is either a free-text note (one blob, first line shown as the title) or a bare link to an issue or PR. It can't hold a title and description, people, labels or a date, and a note can't reference an issue or PR.

## Design

Mocks approved in conversation: list, New project dialog, board with composer and `#` picker, and the side-panel editor.

### 1. List page (`internal/view/pages/projects.templ`)

- The whole row is a link (stretched-link overlay on the title; Delete stays independently clickable).
- "New project" opens a dialog (name, description); create navigates to the new board.

### 2. Card model

A card stays one row in `project_cards` and gains:

| Field | Meaning |
|---|---|
| `title` | Short text shown on the card. Required for a note card. |
| `note` | Reused as the markdown description (column kept, no rename). |
| `due_date` | Optional date. |
| `issue_id` / `pull_id` | Optional link. A note card may now carry one; a card with a link and no title is a plain linked card as today. |
| `card_assignees(card_id, user_id)` | Repo owner or collaborators only. |
| `card_labels(card_id, label_id)` | Labels of the card's own repo only. |

- The `CHECK` in `036_create_projects.sql` ties `note <> ''` to "no link" and forbids a title alongside a link. A new migration replaces it with: at most one of `issue_id`/`pull_id`, and a card with neither has a non-empty `title`.
- Backfill: for existing note cards, `title` is the first line (≤120 chars, as `FirstLine` does today) and `note` keeps the remainder; when the first line is longer than 120 chars, `note` keeps the whole original text so nothing is lost.
- Linked cards without a title show the linked item's own title, state, labels and assignees, read-only. A note card with a link shows its own title, labels and assignees plus the linked item's number and state. The linked item's labels and assignees are not copied.

### 3. API (all under `/api/repos/{owner}/{repo}/projects/{id}`, write access like `CreateCard`)

- `POST /cards`: accepts `title`, `description`, `due_date`, `assignee_ids`, `label_ids`, `issue_id` or `pull_id`. The card modal sends every field for a titled card, or only the link for a title-less linked card.
- `PATCH /cards/{cardID}/details`: replaces the card's fields and assignee/label sets in one transaction. Rejects an empty title on a note card, assignees who are not collaborators, labels from another repo, and a link to another repo (`CardTargetsInRepo`).
- `PATCH /cards/{cardID}/fields`: changes only the fields named in the body (`null`/`""` clears the due date, `null` a link, `[]` a set) and leaves the rest. It merges into the card read under its row lock (`SELECT ... FOR UPDATE`), so two fields saved at once both survive, and checks the merged card with the same rules as `details`. Returns the card with its link, `description_html`, `assignees` and `labels`, which the card modal redraws the field from.
- `GET /card-targets?q=`: issues and PRs of the repo by title fragment or `#number`, newest first, limit 8, write access required. `position()` rather than `ILIKE` so `%` and `_` match literally.
- `POST /cards/{cardID}/convert`: note card only. Creates an issue via `IssueService.Create` from title and description, copies the card's labels and assignees onto it, sets `issue_id` and leaves the card's own title, description, due date, assignees and labels untouched, and returns the card, whose `issue_id`, `issue_number`, `issue_title` and `issue_state` describe the new issue. The card stays a titled card that opens the modal, now with the linked issue chip. `IssueService.Create` commits on its own, so this is create-then-link: if linking fails the new issue is deleted and the card is unchanged. Needs the same write check as creating an issue; visibility is `public`.
- `ListColumnsWithCardsExpanded` / `KanbanCardView` expose title, description, due date, assignees, labels, link number and state.

### 4. `#123` autolinks in descriptions

No existing autolinker: `internal/markdown` has none (closing keywords are parsed separately in `service/closing_keywords.go`). Add a repo-scoped goldmark transformer used when rendering a card description: `#N` becomes a link to that repo's `/issues/N`, or `/pulls/N` if only a PR has that number, and is left as plain text when neither exists. Not applied inside code spans or blocks. Also usable by comments later, but out of scope here.

### 5. Board UI (`project_detail.templ`, `kanban.js`)

- Card face: title, one-line description preview, label chips, due date (danger colour once past), assignee avatars, linked issue/PR number and state. The whole card opens the card modal.
- Issue and PR cards without a title navigate to the issue or PR instead; hidden private-issue placeholders open nothing.
- Card modal: one centered `<dialog>` (960px, body scrolls, footer always visible, close X in the header) for creating a card and for viewing one, two columns from `lg` up (stacked below). Left: Title and markdown Description, written in the issues page's editor (`components.MarkdownEditor`: Write/Preview tabs and toolbar; Preview renders the text as typed through `POST /api/markdown/preview`). Right: Assignees and Labels (multi-select dropdowns, `components.MultiSelect`), Due date (calendar picker, `components.DatePicker`) and Linked item (`#` search picker; the chosen item is a chip that links to the issue or PR, with a separate unlink button).
  - Create: each column's "+ Add item" opens it as a form, "New card in {column}", with every field and Create card / Cancel. Title is required unless an item is linked; a title-less linked card sends only the link. Enter in Title submits.
  - View (a note card, Jira-style): the column name, the title as the heading and the rendered description (`#N` linked), with each field saving on its own through `PATCH .../fields`; there is no Save-all. The title and description each have a pencil that edits them in place with Save and Cancel (Enter saves the title; Escape cancels only that field). The dropdowns save when their popover closes, the due date when a day is picked or cleared, and the link when an item is picked, changed (pencil) or unlinked. Each field shows its own Saving… / Saved / error line (polite live region) and is disabled while it saves; a failed save restores the field (an in-place editor keeps the typed text) and leaves the other fields and the modal alone. The footer has Delete and Convert to issue (titled cards without a link; disabled while a field is edited or saving), both confirmed inline, not with `window.confirm`. While a field saves, the X is disabled and Escape is ignored; closing with a dropdown still open saves it first and closes once that save succeeds (an error keeps the modal open). Convert and Delete likewise save an open dropdown and wait for every save. Closing drops an unsaved in-place edit, and the board reloads on close after any change so the card face is current. A successful Convert keeps the modal open on the new link chip with an inline success line.
  - Read-only users get the same view with no pencils, dropdowns or pickers, and only Close.
  - Escape closes the picker, calendar or dropdown first, then an in-place edit, then a confirmation, then the modal. Focus goes to Title (create), the title pencil (view) or Close (read-only) on open, back to a field's pencil after its Save or Cancel, and back to the opener on close.
- Due date calendar: Monday-first month grid, previous/next month, Today and Clear, keyboard navigation (arrows, PageUp/PageDown, Home/End), `YYYY-MM-DD` value built from local date parts. The picker and calendar popovers are `position: fixed` so the modal body's scroll container does not clip them; the calendar flips above the field when there is no room below.
- Column header: count and a `⋯` menu (delete). "Add column" is a dashed slot after the last column.
- Drag-to-move is unchanged; a drag never opens the modal.

## Out of scope

Custom fields, multiple views, card comments, milestones, `#123` links in comments, and moving cards between projects.

## Risks

- The migration rewrites existing notes; the backfill must be tested on a database with notes, including notes longer than 120 characters in the first line and notes with no newline.
- Deleting a user or label must cascade to the join tables (`ON DELETE CASCADE`).
- Convert creates an issue and edits a card; both must commit together.

## Testing

Service and store tests for each endpoint rule above, a migration backfill test, router tests for permissions (anonymous, outsider, writer), a markdown test for the autolinker (valid issue, valid PR, unknown number, code span), and a browser check on a throwaway server and scratch DB covering list, dialog, card modal create and edit, picker, calendar, link, convert and drag.

## Tickets

`issues/01` list page, `02` card model and migration, `03` card details API, `04` picker endpoint, `05` `#123` autolink, `06` convert to issue, `07` board UI.
