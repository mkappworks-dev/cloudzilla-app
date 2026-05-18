# UI Overhaul · Phase 7 · Gists — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port `gists.templ`, `gist_detail.templ`, `gist_edit.templ`, `gist_new.templ` to match their mockups. Surface star + fork counts on the list (currently neither table exists). Ensure the markdown preview endpoint exists for `gist_new`.

**Architecture:** One new migration (056) to add the missing `gist_stars` table and a `forked_from_id` column on `gists` (verified absent from `migrations/047_create_gists.sql`). Plus class translation on four templ files and small store/handler additions. Migration numbering: 054 is reserved by Phase 1 (`commit_day_counts`), 055 by Phase 3 (`contributor_week_stats`), so Phase 7 owns 056. Re-verify with `ls internal/db/migrations/` before authoring (current highest in `main` is 053).

**Prerequisites:** Phase 6 merged.

> **Revised 2026-05-18 (account-navigation feature).** Two adjustments:
> - `GistStore.CountByOwner` and `GistService.CountByUser` **already exist** (added for the account nav's "Gists" badge). Do NOT redefine them in Task 3 — `ListWithCounts` / `ListPrivateByOwner` are still net-new.
> - The `/gists` handler now attaches `AccountSubnav` via `withAccountSubnav(..., "gists", ...)`. The `gists.templ` port in Task 4 must keep rendering under `AccountSubnav` (the layout renders it from `BasePage.AccountSubnav`); don't strip it.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-7-gists`

---

### Task 1: Branch setup

- [ ] `git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-7-gists`

---

### Task 2: Migration 056 — `gist_stars` + `gists.forked_from_id`

**Files:**
- Create: `internal/db/migrations/056_add_gist_stars_and_forks.sql`

Verified absent from migration 047 — `gists` has columns `(id, owner_id, owner_name, description, public, created_at, updated_at)` only; no `forked_from_id`, no stars table. There is also no `language` column on `gists` and no `language` field on `gist_files` (the language chip in `mockups/gists.html` is derived per-file from the filename extension).

- [ ] **Step 1: Write the migration**

```sql
-- 056_add_gist_stars_and_forks.sql
ALTER TABLE gists ADD COLUMN forked_from_id TEXT NULL REFERENCES gists(id) ON DELETE SET NULL;
CREATE INDEX idx_gists_forked_from ON gists(forked_from_id);

CREATE TABLE gist_stars (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    gist_id    TEXT   NOT NULL REFERENCES gists(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(gist_id, user_id)
);
CREATE INDEX idx_gist_stars_gist ON gist_stars(gist_id);
CREATE INDEX idx_gist_stars_user ON gist_stars(user_id);
```

Note: `gists.id` is `TEXT` (hex-encoded), so `forked_from_id` is `TEXT` to match — not `BIGINT`.

- [ ] **Step 2: Apply & verify**

```bash
make migrate
# expect: "applied migration 056_add_gist_stars_and_forks.sql"
```

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/056_add_gist_stars_and_forks.sql
git commit -m "feat(db): migration 056 — gist_stars table and gists.forked_from_id"
```

---

### Task 3: Surface star + fork counts; add private list filter

**Files:**
- Modify: `internal/store/gist_store.go`
- Modify: `internal/service/gist_service.go`
- Modify: `internal/model/gist.go` (add `ForkedFromID *string` to `Gist`; new `GistListRow` struct for counts)

- [ ] **Step 1: Failing test**

```go
func TestGistService_ListWithCounts(t *testing.T) {
    stores, svcs := testStores(t)
    alice := seedUserSvc(t, stores, "alice")
    g := seedGist(t, stores, alice.ID, "hello")
    seedGistStar(t, stores, g.ID, alice.ID)
    seedGistFork(t, stores, g.ID, "bob") // inserts a gist with forked_from_id=g.ID

    rows, err := svcs.Gist.ListWithCounts(context.Background(), "")
    if err != nil { t.Fatalf("ListWithCounts: %v", err) }
    if len(rows) == 0 || rows[0].ForkCount < 1 || rows[0].StarCount < 1 {
        t.Errorf("expected star+fork >= 1, got %+v", rows)
    }
}
```

- [ ] **Step 2: Implement**

Add a `GistListRow` struct in `internal/model/gist.go` embedding `Gist` plus `FileCount`, `StarCount`, `ForkCount int64`.

Add `GistStore.ListWithCounts(ctx, ownerFilter string)` returning `[]model.GistListRow`. The base column list must match the existing `gists` schema (note: there is no `title` column — `description` is the visible label, and `id` is `TEXT`):

```sql
SELECT g.id, g.owner_id, g.owner_name, g.description, g.public,
       g.created_at, g.updated_at,
       (SELECT COUNT(*) FROM gist_files  WHERE gist_id = g.id) AS file_count,
       (SELECT COUNT(*) FROM gist_stars  WHERE gist_id = g.id) AS star_count,
       (SELECT COUNT(*) FROM gists       WHERE forked_from_id = g.id) AS fork_count
FROM gists g
JOIN users u ON u.id = g.owner_id
WHERE ($1 = '' OR u.username = $1)
ORDER BY g.updated_at DESC
LIMIT 50
```

Also add `GistStore.ListPrivateByOwner(ctx, ownerID, page, pageSize)` — the existing store has `ListByOwner` and `ListPublicByOwner` but no private-only variant. SQL: `WHERE owner_id = $1 AND public = false`. Wrap in `GistService.ListPrivateByOwner` mirroring the existing `ListPublicByOwner` (same page/pageSize clamping).

`GistService.ListWithCounts(ctx, ownerFilter string)` wraps the store call. No page clamping needed (single `LIMIT 50`).

- [ ] **Step 3: Run, verify, commit**

```bash
go test ./internal/service/ -run TestGistService_ListWithCounts -v
git add internal/store/gist_store.go internal/service/gist_service.go internal/model/gist.go
git commit -m "feat(service): GistService ListWithCounts (file/star/fork) + ListPrivateByOwner"
```

---

### Task 4: Port `gists.templ`

**Files:** Modify `internal/view/pages/gists.templ`, handler, viewmodel.

- [ ] **Step 1: Use the new ListWithCounts**

```go
data.Gists, _ = h.Services.Gist.ListWithCounts(ctx, ownerFilter)
```

Update the page viewmodel in `internal/handler/viewmodels.go` from `[]model.Gist` to `[]model.GistListRow`.

- [ ] **Step 2: Body**

Translate classes per the Phase 0 map. Body: tabbed nav (Public / Secret) — only render the Secret tab when the signed-in user is viewing their own gists; non-owners and anonymous viewers see Public only. The Secret tab calls `GistService.ListPrivateByOwner`.

Each gist row shows description (used as title), owner avatar, file count, star count, fork count, language chip, and updated time. The language chip is derived per-row in the handler from the first/most-common file extension in `gist_files` (e.g. `.css` → `CSS` with `lbl-blue`; `.sh` → `Bash` with `lbl-green`; `.sql` → `SQL` with `lbl-amber`). There is no `language` column — confirm by re-reading `internal/db/migrations/047_create_gists.sql` and `internal/model/gist.go`. Expose a small `gistLanguage(files []model.GistFile) (label, chipClass string)` helper in `internal/view/pages/` or `internal/handler/render_helpers.go`. To avoid an N+1 over files for the list view, either (a) add a sibling `LoadFilenames` store method that batches one `SELECT gist_id, filename FROM gist_files WHERE gist_id = ANY($1)` and merge into the rows, or (b) extend `ListWithCounts` to also return a `primary_extension` via a `MIN(filename)`/`ORDER BY id LIMIT 1` correlated subquery. Pick (a) — simpler, keeps the count query unchanged.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/gists.templ internal/view/pages/gists_templ.go internal/handler/
git commit -m "feat(ui): port gists list page (file/star/fork counts + language chip)"
```

---

### Task 5: Port `gist_detail.templ`

**Files:** Modify `internal/view/pages/gist_detail.templ`, handler.

- [ ] **Step 1: Apply class translation**

Body: description + author + visibility chip + clone URL field at top. Then file tabs (one per gist file), each with code body. Comments section below.

- [ ] **Step 2: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/gist_detail.templ internal/view/pages/gist_detail_templ.go
git commit -m "feat(ui): port gist detail page"
```

---

### Task 6: Port `gist_edit.templ` (class translation pass)

**Files:** Modify `internal/view/pages/gist_edit.templ`.

The edit page already exists (verified `ls internal/view/pages/ | grep gist`); it shares the form structure of `gist_new` so apply the same Phase 0 class translation map. Re-verify the inputs (description, public/secret radios, per-file filename + content textareas) match the file shape used by `GistService.Update`.

- [ ] **Step 1: Translate classes**

- [ ] **Step 2: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/gist_edit.templ internal/view/pages/gist_edit_templ.go
git commit -m "feat(ui): port gist edit page"
```

---

### Task 7: Port `gist_new.templ` with Write/Preview tabs

**Files:** Modify `internal/view/pages/gist_new.templ`, possibly `internal/handler/`, possibly `internal/router/router.go`.

- [ ] **Step 1: Confirm or create the markdown preview endpoint**

```bash
grep -rn "PostMarkdown\|/api/markdown\|MarkdownPreview" internal/handler/ internal/router/
```

Verified at plan-time: `internal/markdown.Render` exists and is used by issues, PRs, comments, wiki, releases, and discussion handlers — but no HTTP endpoint exposes it. The grep above returns zero matches for `PostMarkdown` / `/api/markdown`. The endpoint must be added:

1. Create handler in `internal/handler/markdown_handler.go`:

```go
// POST /api/markdown — accepts { "body": "..." }, returns { "html": "..." }
func (h *Handler) PostMarkdown(w http.ResponseWriter, r *http.Request) {
    var body struct{ Body string `json:"body"` }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid body"})
        return
    }
    writeJSON(w, http.StatusOK, struct {
        HTML string `json:"html"`
    }{HTML: markdown.Render(body.Body)})
}
```

2. **Register the route** in `internal/router/router.go` under the authenticated API group: `r.Post("/api/markdown", h.PostMarkdown)`. Confirm grouping matches the rest of `/api/*` (auth-required, JSON). This is a real step, not optional — verify with `grep -n "/api/markdown" internal/router/router.go` after editing.

- [ ] **Step 2: Body**

Per file row (HTMX-add another file row on demand): filename input, language select, Write/Preview tab toggle (Alpine-driven), textarea (Write) or rendered HTML pane (Preview, fetched via HTMX `hx-post="/api/markdown"`). Below: description input, public/secret radios, Create button.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/gist_new.templ internal/view/pages/gist_new_templ.go internal/handler/ internal/router/router.go
git commit -m "feat(ui): port new-gist page with Write/Preview tabs + /api/markdown"
```

---

### Task 8: Verify and open PR

Tests + lint + templ regen + visual sweep across all four gist pages (list, detail, edit, new). Run silent-failure-hunter. PR title: `feat(ui): UI overhaul phase 7 — gists`. PR body lists migration 056 and the new `/api/markdown` endpoint.

---

## Self-review checklist

- [ ] Migration 056 applies cleanly and is sequential after 055.
- [ ] `forked_from_id` typed `TEXT` (not `BIGINT`) to match `gists.id`.
- [ ] `ListWithCounts` is a single SQL query for counts (filename N+1 avoided via batched `ANY($1)` fetch).
- [ ] `ListPrivateByOwner` exists and is only callable for the gist's owner.
- [ ] Markdown preview endpoint reuses `internal/markdown.Render` (same renderer as wiki/issues/PRs) and is registered under auth-required `/api/*`.
- [ ] Language chip on the list is derived from filename extension, not a stored `language` column.
- [ ] Public-vs-Secret tab on `/gists` defaults to Public; Secret tab is hidden for non-owners.
- [ ] `gist_edit.templ` received the same Phase 0 class translation pass as the other three pages.
