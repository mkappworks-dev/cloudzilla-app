# Projects: GitHub-style list, board and rich cards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Clickable project rows, a New project dialog, and a board whose cards save a title, markdown description, assignees, labels, due date and an optional issue/PR link, edited in a side panel, with `#123` autolinks and convert-to-issue.

**Architecture:** Stores → Services → Handlers as in `CLAUDE.md`. `project_cards` gains `title` and `due_date`; `note` becomes the description; `card_assignees` and `card_labels` hold people and labels. A card's link no longer excludes a title. The board is server-rendered Templ with Alpine components in `cmd/server/frontend/static/kanban.js`.

**Tech Stack:** Go (chi, database/sql), PostgreSQL, Templ, Alpine.js, Tailwind v4, goldmark.

**Spec:** `.scratch/2026-10-09-projects-github-style/spec.md` (tickets in `.scratch/2026-10-09-projects-github-style/issues/`).

## Global Constraints

- Layering: `internal/store/` raw SQL, `internal/service/` logic, `internal/handler/` calls services only; `context.Context` first arg everywhere.
- Edit `.templ` files then run `~/go/bin/templ generate ./internal/view/...`; never hand-edit `_templ.go`. Never run `templ fmt`.
- Tailwind v4 utilities only; classes must appear in `internal/view/**/*.templ`. Rebuild CSS with `bin/tailwindcss -i tailwind/input.css -o cmd/server/frontend/static/main.css --minify` (binary lives in the main checkout's `bin/`).
- Static assets are referenced with `assets.URL("/static/x.js")`.
- Comments only for the why the code can't show; one line by default; no task or phase narration.
- Commit messages end with `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`. Never stage `.claude/`; always `git add` specific paths. Never run `git stash`.
- Integration tests need a DB: `TEST_DATABASE_DSN="postgres://cloudzilla:test@localhost:5497/cloudzilla_test?sslmode=disable"` (container `cz-projects-ui-test-db`, already migrated and seeded; re-run `CZ_DATABASE_DSN=<same> go run ./cmd/cz-admin/. migrate` after adding a migration). Do not use :5432 or :5496.
- Bash guard in this worktree: use literal paths, no `$(...)`, loops or heredocs; write files with Write/Edit.
- Never seed or delete rows in the shared dev DB; browser checks run on a throwaway server (`CZ_SERVER_PORT=8097 CZ_AUTH_COOKIE_NAME=cz_projects_ui`, dsn above).
- Sign-in for local checks: `admin@example.test` with the seed password; the seeded repos are under `siteadmin` (e.g. `mellow-beacon`).

## File Structure

| File | Responsibility |
|---|---|
| `internal/db/migrations/111_project_card_details.sql` | New columns, constraint, join tables, backfill |
| `internal/model/project.go` | `ProjectCard` fields, `CardDetails`, `CardUser`, `CardTarget` |
| `internal/store/project_store.go` | Card CRUD with new fields, assignee/label sets, ref kinds |
| `internal/service/project_service.go` | `CreateCard`, `UpdateCardDetails`, `SearchCardTargets`, board view |
| `internal/service/project_convert.go` (new) | `ConvertCardToIssue` |
| `internal/handler/project_handler.go` | Routes' handlers |
| `internal/markdown/refs.go` (new) | `RefNumbers`, `RenderWithRefs` |
| `internal/view/pages/projects.templ`, `project_detail.templ` | List and board |
| `cmd/server/frontend/static/kanban.js` | `kanbanBoard`, `cardComposer`, `cardPanel` |

---

### Task 1: Land the list page, picker endpoint and composer already in the tree

Covers tickets 01 and 04. The working tree already holds the clickable list rows, New project dialog, `card-targets` endpoint and tests, and a first board UI. This task verifies and checkpoints them; Task 6 replaces the board UI parts.

**Files:**
- Modify (already edited): `internal/view/pages/projects.templ`, `internal/model/project.go`, `internal/store/project_store.go`, `internal/service/project_service.go`, `internal/handler/project_handler.go`, `internal/router/router.go`, `internal/router/project_routes_test.go`, `internal/view/pages/project_detail.templ`, `cmd/server/frontend/static/kanban.js`, and the regenerated `*_templ.go`.

**Interfaces:**
- Produces: `GET /api/repos/{owner}/{repo}/projects/{id}/card-targets?q=` → `[]model.CardTarget{ID int64, Kind "issue"|"pull", Number int, Title, State string}`; `ProjectService.SearchCardTargets(ctx, projectID, userID int64, query string) ([]model.CardTarget, error)`; `ProjectStore.SearchCardTargets(ctx, repoID int64, query string, number, limit int)`.

- [ ] **Step 1: Run the project tests**

Run: `TEST_DATABASE_DSN="postgres://cloudzilla:test@localhost:5497/cloudzilla_test?sslmode=disable" go test ./internal/router/ ./internal/service/ ./internal/store/ ./internal/view/... -count=1`
Expected: PASS.

- [ ] **Step 2: Add `ErrEmptyNote` removal note**

Leave `UpdateCardNote` and `PATCH .../note` in place; Task 3 deletes them.

- [ ] **Step 3: Checkpoint commit**

```bash
git add cmd/server/frontend/static/kanban.js internal/handler/project_handler.go internal/model/project.go internal/router/project_routes_test.go internal/router/router.go internal/service/project_service.go internal/store/project_store.go internal/view/pages/project_detail.templ internal/view/pages/project_detail_templ.go internal/view/pages/projects.templ internal/view/pages/projects_templ.go
git commit -m "feat(projects): clickable project rows, New project dialog and issue/PR picker"
```

---

### Task 2: Card model and migration

Covers ticket 02.

**Files:**
- Create: `internal/db/migrations/111_project_card_details.sql`
- Modify: `internal/model/project.go`, `internal/store/project_store.go`, `internal/service/project_service.go` (`KanbanCardView`)
- Test: `internal/store/project_store_test.go` (follow its existing helpers), `internal/db/` migration test if one exists for backfills (search `internal/db/*_test.go`)

**Interfaces:**
- Produces:
  - `model.ProjectCard` gains `Title string \`db:"title" json:"title"\`` and `DueDate *time.Time \`db:"due_date" json:"due_date,omitempty"\``.
  - `model.CardUser struct{ ID int64; Username string }` with json tags `id`, `username`.
  - `model.CardDetails struct{ Title, Description string; DueDate *time.Time; IssueID, PullID *int64; AssigneeIDs, LabelIDs []int64 }`.
  - `(*ProjectStore).CreateCard(ctx, card *model.ProjectCard) error` now inserts `title`, `due_date`.
  - `(*ProjectStore).SetCardDetails(ctx, cardID, projectID int64, d model.CardDetails) error` — one transaction: updates `title`, `note`(=Description), `due_date`, `issue_id`, `pull_id`, replaces `card_assignees` and `card_labels`; returns `ErrCardNotInProject` when the card is not in the project.
  - `(*ProjectStore).CardAssignees(ctx, cardIDs []int64) (map[int64][]model.CardUser, error)`
  - `(*ProjectStore).CardLabels(ctx, cardIDs []int64) (map[int64][]model.Label, error)`
  - `(*ProjectStore).LabelsInRepo(ctx, repoID int64, labelIDs []int64) (bool, error)`
  - `ListCardsByColumn` selects `c.title, c.due_date` too.

- [ ] **Step 1: Write the migration**

Check the constraint name first with `docker exec cz-projects-ui-test-db psql -U cloudzilla -d cloudzilla_test -c "\d project_cards"`; the unnamed CHECK is `project_cards_check`.

```sql
ALTER TABLE project_cards ADD COLUMN title    TEXT NOT NULL DEFAULT '';
ALTER TABLE project_cards ADD COLUMN due_date DATE;

-- A first line over 120 chars keeps the whole original text as the description so nothing is lost.
UPDATE project_cards SET
    title = LEFT(split_part(note, E'\n', 1), 120),
    note  = CASE
        WHEN length(split_part(note, E'\n', 1)) > 120 THEN note
        WHEN position(E'\n' IN note) > 0 THEN substr(note, length(split_part(note, E'\n', 1)) + 2)
        ELSE ''
    END
WHERE issue_id IS NULL AND pull_id IS NULL;

ALTER TABLE project_cards DROP CONSTRAINT project_cards_check;
ALTER TABLE project_cards ADD CONSTRAINT project_cards_shape CHECK (
    (issue_id IS NULL OR pull_id IS NULL)
    AND (issue_id IS NOT NULL OR pull_id IS NOT NULL OR title <> '')
);

CREATE TABLE card_assignees (
    card_id BIGINT NOT NULL REFERENCES project_cards(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, user_id)
);

CREATE TABLE card_labels (
    card_id  BIGINT NOT NULL REFERENCES project_cards(id) ON DELETE CASCADE,
    label_id BIGINT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, label_id)
);
```

- [ ] **Step 2: Write the failing store tests**

In `internal/store/project_store_test.go`, using that file's repo/project/column helpers, add tests that: (a) `CreateCard` with `Title: "t", Note: "d"` round-trips through `ListCardsByColumn`; (b) a card with `IssueID` and a non-empty `Title` is accepted; (c) a card with neither link nor title is rejected by the DB; (d) `SetCardDetails` replaces assignee and label sets and updates `due_date`, and a second call with empty sets clears them; (e) `SetCardDetails` on a card of another project returns `ErrCardNotInProject`; (f) deleting a label removes its `card_labels` row (CASCADE).

- [ ] **Step 3: Run to verify failure**

Run: `TEST_DATABASE_DSN=… go test ./internal/store/ -run 'Card' -count=1`
Expected: FAIL (columns and methods missing).

- [ ] **Step 4: Implement**

Apply the migration to the test DB (`CZ_DATABASE_DSN=… go run ./cmd/cz-admin/. migrate`). Add the model fields and types. Update `CreateCard` to `INSERT INTO project_cards (column_id, issue_id, pull_id, title, note, due_date, position) VALUES ($1,$2,$3,$4,$5,$6, COALESCE((SELECT MAX(position)+1 FROM project_cards WHERE column_id = $1), 0))`. Implement `SetCardDetails` with `BeginTx`: `UPDATE project_cards SET title=$3, note=$4, due_date=$5, issue_id=$6, pull_id=$7 WHERE id=$1 AND column_id IN (SELECT id FROM project_columns WHERE project_id=$2)`; zero rows → `ErrCardNotInProject`; then `DELETE FROM card_assignees WHERE card_id=$1` and re-insert each id, same for `card_labels`. Batch readers use `WHERE card_id = ANY($1)` with `pq.Array` (check which array helper `internal/store` already uses; `CLAUDE.md` says build `IN (...)` with numbered params if not). Add `title`, `due_date` to `ListCardsByColumn` and its `Scan`. Add to `KanbanCardView`: `Title` stays the display title; add `Description string`, `DueDate string` (`2006-01-02` or empty), `Overdue bool`, `Assignees []model.CardUser`, `Labels []model.Label`, `LinkKind string`, `LinkNumber int`, `LinkState string`, `Kind` stays `"issue" | "pull" | "note"` where a card with a non-empty `Title` is `"note"` even when linked. `ListColumnsWithCardsExpanded` fills them using `CardAssignees` and `CardLabels` once for all card ids. `Note` on the view is replaced by `Description`.

- [ ] **Step 5: Run tests**

Run: `TEST_DATABASE_DSN=… go test ./internal/store/ ./internal/service/ ./internal/db/ -count=1`
Expected: PASS. Fix call sites of changed struct fields.

- [ ] **Step 6: Commit**

```bash
git add internal/db/migrations/111_project_card_details.sql internal/model/project.go internal/store/project_store.go internal/store/project_store_test.go internal/service/project_service.go
git commit -m "feat(projects): add title, due date, assignees and labels to cards"
```

---

### Task 3: Card details API

Covers ticket 03. Replaces `UpdateCardNote` and `PATCH .../note`.

**Files:**
- Modify: `internal/service/project_service.go`, `internal/handler/project_handler.go`, `internal/router/router.go`
- Test: `internal/service/project_service_boards_test.go` (existing patterns), `internal/router/project_routes_test.go`

**Interfaces:**
- Consumes: Task 2's `CardDetails`, `SetCardDetails`, `LabelsInRepo`.
- Produces:
  - `service.ErrInvalidCard = errors.New("invalid card")` (400), `ErrInvalidAssignee`, `ErrInvalidLabel` (both 400); `writeProjectError` maps all three to 400.
  - `(*ProjectService).CreateCard(ctx, projectID, columnID, userID int64, d model.CardDetails) (*model.ProjectCard, error)` — replaces the old five-argument form (update every caller and test).
  - `(*ProjectService).UpdateCardDetails(ctx, projectID, cardID, userID int64, d model.CardDetails) error`
  - Routes: `POST /{id}/cards` body `{column_id, title, description, due_date "YYYY-MM-DD", assignee_ids, label_ids, issue_id, pull_id}`; `PATCH /{id}/cards/{cardID}/details` same fields without `column_id`; `PATCH /{id}/cards/{cardID}` stays the move.

- [ ] **Step 1: Write failing router tests**

In `internal/router/project_routes_test.go` replace `TestProjects_EditNote` with `TestProjects_CardDetails`. Seed a label (`INSERT INTO labels (repo_id, name) …` via `e.db`), reuse `e.writer`, `e.outsider`. Cases for `PATCH e.path("/projects/%d/cards/%d/details", p, card)`:

| name | want |
|---|---|
| anonymous | 401 |
| outsider | 403 |
| bad json | 400 |
| blank title on a note card | 400 |
| assignee who is not owner or collaborator (`e.outsider.id`) | 400 |
| label of another repo | 400 |
| issue of another repo | 404 |
| card of another project | 404 |
| bad due date `"31/12"` | 400 |
| writer with title, description, due date, assignee `e.writer.id`, label, issue link | 204, and rows in `project_cards`, `card_assignees`, `card_labels` match |
| second call with empty arrays | 204 and join rows gone |

Also assert `POST /cards` still accepts `{"column_id":X,"title":"x"}` and `{"column_id":X,"issue_id":N}` and rejects `{"column_id":X}` with 400.

- [ ] **Step 2: Run to verify failure**

Run: `TEST_DATABASE_DSN=… go test ./internal/router/ -run 'TestProjects_' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement the service**

```go
func (s *ProjectService) validateDetails(ctx context.Context, repo *model.Repository, d model.CardDetails, isLinkOnlyAllowed bool) error {
	if d.IssueID != nil && d.PullID != nil {
		return ErrInvalidCard
	}
	linked := d.IssueID != nil || d.PullID != nil
	if strings.TrimSpace(d.Title) == "" && !(linked && isLinkOnlyAllowed) {
		return ErrInvalidCard
	}
	if len([]rune(d.Title)) > MaxTitleLen {
		return ErrInvalidCard
	}
	inRepo, err := s.projects.CardTargetsInRepo(ctx, repo.ID, d.IssueID, d.PullID)
	if err != nil {
		return err
	}
	if !inRepo {
		return ErrCardTargetNotFound
	}
	if len(d.AssigneeIDs) > 0 {
		allowed := map[int64]bool{repo.OwnerID: true}
		perms, err := s.repos.ListCollaborators(ctx, repo.ID)
		if err != nil {
			return err
		}
		for _, p := range perms {
			allowed[p.UserID] = true
		}
		for _, id := range d.AssigneeIDs {
			if !allowed[id] {
				return ErrInvalidAssignee
			}
		}
	}
	if len(d.LabelIDs) > 0 {
		ok, err := s.projects.LabelsInRepo(ctx, repo.ID, d.LabelIDs)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidLabel
		}
	}
	return nil
}
```

`MaxTitleLen` already exists in the service package (used by `IssueService`); confirm with `grep -n MaxTitleLen internal/service`. `CreateCard` keeps the existing column/project and write checks, calls `validateDetails(..., true)` (a link-only card is allowed), builds the `model.ProjectCard` from `d`, calls `projects.CreateCard`, then `SetCardDetails` only if assignees or labels are present (or call a shared insert helper), then `TouchProject`. `UpdateCardDetails` does the write check, `validateDetails(..., false)` for a card whose stored `title` is non-empty; for a plain linked card (stored title empty) allow link-only. Load the card's current title with a small `ProjectStore.GetCardInProject(ctx, cardID, projectID) (*model.ProjectCard, error)` and pick the flag from it. Map `store.ErrCardNotInProject` to `ErrProjectNotFound`. Delete `UpdateCardNote`, `ErrEmptyNote` and `ProjectStore.UpdateCardNote`.

- [ ] **Step 4: Implement handlers and route**

Decode into a struct with `DueDate string`; parse with `time.Parse("2006-01-02", …)` and return 400 on error; empty string means nil. Replace `UpdateCardNote` handler with `UpdateCardDetails` (reads `cardID` param like `MoveCard`). In `router.go` replace the `/note` route with `r.With(authMW).Patch("/{id}/cards/{cardID}/details", h.UpdateCardDetails)`. Add `ErrInvalidCard`, `ErrInvalidAssignee`, `ErrInvalidLabel` to the 400 branch of `writeProjectError`.

- [ ] **Step 5: Run tests**

Run: `TEST_DATABASE_DSN=… go test ./internal/router/ ./internal/service/ ./internal/store/ ./internal/handler/ -count=1`
Expected: PASS.

- [ ] **Step 6: Document and commit**

Add the new and changed routes to `docs/api-reference.md` (project section near line 438) and `docs/access-control.md` (route table near line 683).

```bash
git add internal/service internal/handler internal/router internal/store docs/api-reference.md docs/access-control.md
git commit -m "feat(projects): save title, description, people, labels and link on cards"
```

---

### Task 4: `#123` autolinks in descriptions

Covers ticket 05. Independent of Tasks 2–3; run after Task 3 to avoid git index races.

**Files:**
- Create: `internal/markdown/refs.go`, `internal/markdown/refs_test.go`
- Modify: `internal/store/project_store.go` (`RefKinds`), `internal/service/project_service.go` (`DescriptionHTML` on the board view)

**Interfaces:**
- Produces:
  - `markdown.RefNumbers(src string) []int` — distinct `#N` numbers found in the raw text, in order.
  - `markdown.RenderWithRefs(ctx context.Context, src, repoBase string, kinds map[int]string) string` — `kinds[n]` is `"issues"`, `"pulls"` or absent; links `#N` to `repoBase + "/" + kind + "/" + N`.
  - `(*ProjectStore).RefKinds(ctx, repoID int64, nums []int) (map[int]string, error)` — issues win over PRs when both have the number.
  - `KanbanCardView.DescriptionHTML string` filled by `ListColumnsWithCardsExpanded` (one `RefKinds` call for the whole board).

- [ ] **Step 1: Write failing markdown tests**

`internal/markdown/refs_test.go`:

```go
package markdown

import (
	"context"
	"strings"
	"testing"
)

func TestRefNumbers(t *testing.T) {
	got := RefNumbers("see #12, #7 and #12 again; a#3 and &#38; are not refs")
	want := []int{12, 7}
	if len(got) != len(want) || got[0] != 12 || got[1] != 7 {
		t.Fatalf("RefNumbers = %v, want %v", got, want)
	}
}

func TestRenderWithRefs(t *testing.T) {
	kinds := map[int]string{5: "issues", 6: "pulls"}
	cases := []struct{ name, src, want, notWant string }{
		{"issue", "fixes #5", `href="/o/r/issues/5"`, ""},
		{"pull", "see #6", `href="/o/r/pulls/6"`, ""},
		{"unknown number stays text", "see #99", "#99", "href=\"/o/r/issues/99\""},
		{"code span untouched", "run `#5`", "<code>#5</code>", `href="/o/r/issues/5"`},
		{"fenced code untouched", "```\n#5\n```", "#5", `href="/o/r/issues/5"`},
		{"existing link untouched", "[#5](https://example.com)", `href="https://example.com"`, `href="/o/r/issues/5"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := RenderWithRefs(context.Background(), c.src, "/o/r", kinds)
			if !strings.Contains(out, c.want) {
				t.Errorf("output %q missing %q", out, c.want)
			}
			if c.notWant != "" && strings.Contains(out, c.notWant) {
				t.Errorf("output %q contains %q", out, c.notWant)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/markdown/ -run 'Ref' -count=1`
Expected: FAIL (undefined).

- [ ] **Step 3: Implement**

`refs.go` holds `var refRe = regexp.MustCompile(\`(^|[^\w&/#])#(\d+)\b\`)`. `RefNumbers` scans `src` with `FindAllStringSubmatch` keeping first-seen distinct numbers. `RenderWithRefs` builds the same parser as `RenderCtx` plus a second AST transformer `refLinker{base string, kinds map[int]string}`: walk with `ast.Walk`, on each `*ast.Text` whose parent is not `*ast.Link`, `*ast.CodeSpan`, `*ast.AutoLink` or inside a code block (`ast.CodeBlock`/`ast.FencedCodeBlock` parents hold `Lines`, not `Text`, so they are already skipped; verify with the fenced-code test), split the segment text on `refRe` and replace matched `#N` with an `ast.Link` child whose `Destination` is the target and whose text child is `#N`, only when `kinds[N] != ""`. To avoid duplicating the renderer setup, refactor `RenderCtx` into a private `render(ctx, src, extra ...parser.Option)` that both call.

`RefKinds` store method:

```go
func (s *ProjectStore) RefKinds(ctx context.Context, repoID int64, nums []int) (map[int]string, error) {
	out := map[int]string{}
	if len(nums) == 0 {
		return out, nil
	}
	args := []any{repoID}
	ph := make([]string, len(nums))
	for i, n := range nums {
		args = append(args, n)
		ph[i] = fmt.Sprintf("$%d", i+2)
	}
	in := strings.Join(ph, ",")
	rows, err := s.db.QueryContext(ctx,
		`SELECT number, 'pulls' FROM pull_requests WHERE repo_id = $1 AND number IN (`+in+`)
		 UNION ALL
		 SELECT number, 'issues' FROM issues WHERE repo_id = $1 AND number IN (`+in+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n int
		var kind string
		if err := rows.Scan(&n, &kind); err != nil {
			return nil, err
		}
		if out[n] != "pulls" && kind == "issues" || out[n] == "" {
			out[n] = kind
		}
	}
	return out, rows.Err()
}
```

(Add `strings` to the imports.) In `ListColumnsWithCardsExpanded` gather `markdown.RefNumbers` over every non-empty description, call `RefKinds` once, then set `DescriptionHTML = markdown.RenderWithRefs(ctx, desc, "/"+repo.OwnerName+"/"+repo.Name, kinds)`. Add a store test for `RefKinds` (issue only, PR only, both, unknown).

- [ ] **Step 4: Run tests**

Run: `TEST_DATABASE_DSN=… go test ./internal/markdown/ ./internal/store/ ./internal/service/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/markdown internal/store internal/service
git commit -m "feat(projects): link #123 references in card descriptions"
```

---

### Task 5: Convert a note card to an issue

Covers ticket 06.

**Files:**
- Create: `internal/service/project_convert.go`, `internal/service/project_convert_test.go`
- Modify: `internal/service/project_service.go` (`WithIssueService`), `internal/service/services.go` (wire), `internal/handler/project_handler.go`, `internal/router/router.go`, `internal/router/project_routes_test.go`

**Interfaces:**
- Consumes: Task 2/3 store and service methods; `IssueService.Create(ctx, owner, repoName string, authorID int64, title, body, visibility string) (*model.Issue, error)`.
- Produces: `(*ProjectService).ConvertCardToIssue(ctx, projectID, cardID, userID int64) (*model.ProjectCard, error)`; `POST /api/repos/{owner}/{repo}/projects/{id}/cards/{cardID}/convert` → 200 with the card JSON; `service.ErrNotConvertible` (400).

Behavior: only a card with a non-empty title and no link converts. Steps inside the service: write check; load card via `GetCardInProject`; `IssueService.Create` with the card's title, description and visibility `"public"`; copy labels with the label store's `AddToIssue` and assignees with the assignee store's add method (read `internal/store/assignee_store.go` for its name); then `SetCardDetails` with `IssueID` set, `Title` and `Description` empty and the same due date, assignee and label sets cleared (the issue now owns them). If linking the card fails, delete the new issue (use an existing `IssueStore` delete; if none exists add `DeleteByID`) and return the error, so a failure leaves the card unchanged and no orphan issue.

- [ ] **Step 1: Write failing tests**

Router test `TestProjects_ConvertCard`: anonymous 401, outsider 403, a linked card 400, a card from another project 404; a writer converting a titled card with a label and an assignee returns 200, the card now has `issue_id` set and empty title and note, the issue exists in the repo with the same title and body and has the label and the assignee, and `card_labels`/`card_assignees` rows for the card are gone. Service-level rollback test: wrap a failing `SetCardDetails` (card deleted between steps is hard to simulate; instead make the label copy fail by passing a card whose label belongs to another repo inserted directly via SQL) and assert no new issue remains.

- [ ] **Step 2: Run to verify failure**

Run: `TEST_DATABASE_DSN=… go test ./internal/router/ -run 'ConvertCard' -count=1`
Expected: FAIL (404 route).

- [ ] **Step 3: Implement**

Add `issues *IssueService` to `ProjectService` with `WithIssueService`, wire it where `NewProjectService` is called in `services.go` after the issue service exists. Implement `ConvertCardToIssue` as described, the handler (`projectIDInRepo`, parse `cardID`, call, `writeJSON(w, http.StatusOK, card)`), and the route `r.With(authMW).Post("/{id}/cards/{cardID}/convert", h.ConvertCard)`. Map `ErrNotConvertible` to 400 in `writeProjectError`. Update `docs/api-reference.md`.

- [ ] **Step 4: Run tests**

Run: `TEST_DATABASE_DSN=… go test ./internal/router/ ./internal/service/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal docs/api-reference.md
git commit -m "feat(projects): convert a note card into an issue"
```

---

### Task 6: Board UI — rich cards, composer and side panel

Covers ticket 07. Reworks `project_detail.templ` and `kanban.js` from Task 1's first pass to the approved mocks.

**Files:**
- Modify: `internal/view/pages/project_detail.templ`, `internal/view/viewmodels_*.go` (add repo labels and collaborators to `ProjectDetailData`), `internal/handler/project_handler.go` (`PageProjectDetail` fills them), `cmd/server/frontend/static/kanban.js`
- Test: `internal/view/pages/project_detail_test.go` if a pattern exists (see `internal/view/pages/*_test.go`), plus the browser check below

**Interfaces:**
- Consumes: Task 2 `KanbanCardView` fields, Task 3 `PATCH .../details` and `POST .../cards`, Task 4 `DescriptionHTML`, Task 5 `POST .../convert`, Task 1 `GET .../card-targets`.
- `ProjectDetailData` gains `Labels []model.Label` (`LabelService`/store `ListByRepo`) and `People []model.CardUser` (owner plus collaborators with usernames).

- [ ] **Step 1: Data for the panel**

In `PageProjectDetail` load `labels` via the existing label service for the repo and `people` from `Services.Repo.ListCollaborators` plus the owner, only when `canWrite`. Add the fields to `view.ProjectDetailData`.

- [ ] **Step 2: Card face**

Per card render, in this order: title (or the linked item's title for a plain linked card, as today); a one-line `line-clamp-1` description preview in `text-muted-foreground`; label chips using each label's `Color` as an inline `style` background with readable text; a footer row with the due date (`text-destructive` when `card.Overdue`) on the left and overlapping assignee avatars on the right (use `components.Avatar`; read `docs/storage.md` for its context lookup); a linked-item badge (`#N` plus the existing state badge) when `LinkKind != ""`. Plain linked cards keep the stretched-link anchor; every other card is `role="button" tabindex="0"` and opens the panel on click or Enter. Put `data-card-json` on the `li` with the card's editable fields as escaped JSON via a `templ.JSONString` helper (or `data-*` attributes) so the panel needs no extra request.

- [ ] **Step 3: Side panel**

Replace the `note-dialog` with a right-hand panel inside the board root: `<aside x-show="panel.open" x-cloak class="fixed inset-y-0 right-0 z-30 w-[380px] max-w-full border-l border-border bg-background p-4 overflow-y-auto">` (fixed positioning is fine in the real app; the mock widget rule does not apply). Contents in order: header with "Note in {column}" and a close button; title input; a Write/Preview toggle over the description textarea, where Preview shows `DescriptionHTML` from a `<template id="desc-{cardID}">` rendered with `templ.Raw` (trusted markdown output) and says "Save to refresh the preview" after edits; a Linked item row with the `#` picker (reuse the composer's search, clear button); Assignees as a checkbox list from `People`; Labels as a checkbox list from `Labels`; a date input for the due date; footer with Save, Cancel, Delete, and Convert to issue (shown only when the card has a title and no link). Read-only users get the same panel with inputs `disabled` and no footer buttons except Close. Save sends `PATCH .../cards/{id}/details` with `{title, description, due_date, assignee_ids, label_ids, issue_id|pull_id}` then reloads; Delete confirms then `DELETE .../cards/{id}`; Convert confirms then `POST .../cards/{id}/convert` then reloads.

- [ ] **Step 4: Composer and picker**

The composer sends `{column_id, title}` on Enter and `{column_id, issue_id|pull_id}` on a picked result. Fix the clipped dropdown: the board row is `overflow-x-auto`, which forces vertical clipping, so render the results list with `position: fixed` and coordinates from the textarea's `getBoundingClientRect()` set when the list opens and on scroll, instead of `absolute` inside the column. Keep Escape-to-close, arrow keys and `@mousedown.prevent` selection.

- [ ] **Step 5: Regenerate and verify the build**

Run: `~/go/bin/templ generate ./internal/view/...` then `go build ./...` then `bin/tailwindcss` (from the main checkout) as in Global Constraints.
Expected: no errors.

- [ ] **Step 6: Browser check on the throwaway server**

Start the server with the Global Constraints env, sign in, open a `siteadmin` repo's board. Check, asserting state with `Alpine.$data` and screenshots (the pane may be hidden): create a board via the dialog; add a column; add a card with Enter; open the panel and set title, description containing `#1`, an assignee, a label, a due date in the past and a link via `#`; save and confirm the card face shows each; open the Preview and confirm `#1` is a link; convert a note card and confirm it becomes a plain linked card whose issue page shows the title; drag a card to another column and confirm the panel did not open; sign out and confirm the panel is read-only; confirm the picker dropdown shows all eight results unclipped. Take screenshots of the list, the board with rich cards, and the open panel.

- [ ] **Step 7: Full suite and commit**

Run: `TEST_DATABASE_DSN=… go test ./... -count=1`
Expected: PASS.

```bash
git add internal cmd/server/frontend/static/kanban.js
git commit -m "feat(projects): rich cards, composer and side panel on the board"
```

---

### Task 7: Docs, tickets and cleanup

**Files:**
- Modify: `docs/api-reference.md`, `docs/access-control.md`, the seven tickets and the spec in `.scratch/2026-10-09-projects-github-style/`

- [ ] **Step 1: Tick the tickets**

Tick every acceptance criterion that now holds and set `Status: done` on each ticket and on the spec.

- [ ] **Step 2: Final checks**

Run: `golangci-lint run ./...` and `TEST_DATABASE_DSN=… go test ./... -count=1`.
Expected: no findings, all PASS.

- [ ] **Step 3: Commit**

```bash
git add docs .scratch/2026-10-09-projects-github-style
git commit -m "docs(projects): document the card API and close the tickets"
```
