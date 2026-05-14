# UI Overhaul · Phase 9 · Profile & Organizations — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape `user.templ` overview tab (profile mockup), reshape `org.templ` listing/detail to match `mockups/organizations.html`, add `new_organization` page + route (not currently routed), port `repo_new.templ`. Add pinned-repos data model. Add `PinnedRepo` component.

**Architecture:** Pinned repos stored as `users.pinned_repo_ids BIGINT[]` (recommended per spec) — small additive migration. Profile uses the `Heatmap` component from Phase 1 with 52 weeks of repo-scoped commit data for the profile owner.

**Subnav alignment:** The user profile overview tab is rendered under the `DashboardSubnav` chrome with `Active: "overview"`. The organizations listing at `/settings/organizations` is account-settings chrome, NOT `DashboardSubnav` — it lives under the account-settings sidebar. The `new_organization` page renders with the standard global header (no subnav, like new_repo).

**Codebase verification (performed during plan review):**
- Highest existing migration: `053_create_dependency_graph.sql` (verified via `ls internal/db/migrations/`).
- Phase 1 migration is 054, Phase 3 is 055, Phase 7 is 056, Phase 8 is 057. Phase 9's migration is therefore **058**.
- `OrgService.ListForUser` / `OrgService.CountMembers` — **do not exist**. Only `ListOwnedByUser(ctx, userID)` (`internal/service/org_service.go:59`). Both must be added as real sub-tasks (see Task 6.1).
- `EventService.RecentForUserActivity` — **does not exist**. The real method is `EventService.UserActivity(ctx, username string, page, pageSize int)` (`internal/service/event_service.go:76`); it takes a **username**, not a user ID.
- `LanguageService` — **does not exist** as a Go service at all. We will add it as a new service in Task 5.0 (see below); its `AggregateForUser(ctx, userID, limit)` walks the user's owned repos via go-git blob streams.
- `Repository.PrimaryLanguage` — **not on the struct** (`internal/model/repo.go`). Phase 8's revision recommends adding it; if Phase 8 ships that column, Phase 9 reads it directly. **If Phase 8 does NOT add it**, Phase 9 derives the language ad-hoc via the new `LanguageService.Composition(repoPath, ref).TopLanguage()`. Phase 9 plan assumes Phase 8 added the column and falls back to derivation otherwise — see Task 5 step 2.
- `Repository.StarCount` — **not on the base struct**; it lives on `RepositoryWithStats` only. Phase 9 must query `StarStore.CountByRepo(ctx, repoID)` to populate pinned cards (no model change needed).
- `/organizations/new` — **not currently routed**. Plan adds the route (`Task 7 step 1`).

**Prerequisites:** Phase 8 merged.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-9-profile-orgs`

---

### Task 1: Branch setup

- [ ] `git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-9-profile-orgs`

---

### Task 2: Migration (additive) — pinned repos

**Files:**
- Create: `internal/db/migrations/058_add_pinned_repos_to_users.sql`

(Highest existing migration is `053_create_dependency_graph.sql`; Phases 1, 3, 7, 8 consume 054–057.)

- [ ] **Step 1: Write the migration**

```sql
-- 058_add_pinned_repos_to_users.sql
-- Pinned repository IDs are stored as a BIGINT[] column on users so the
-- profile page reads them in a single query alongside the user. Ordering
-- in the array is preserved as the pin order.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS pinned_repo_ids BIGINT[] NOT NULL DEFAULT '{}';
```

- [ ] **Step 2: Apply + verify**

```bash
make migrate
psql "$CZ_DATABASE_DSN" -c '\d users' | grep pinned_repo_ids
```

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/058_add_pinned_repos_to_users.sql
git commit -m "feat(db): migration 058 — add users.pinned_repo_ids"
```

---

### Task 3: Pin/unpin store + service methods — TDD

**Files:**
- Modify: `internal/store/user_store.go`
- Modify: `internal/service/user_service.go`
- Create: `internal/service/user_service_pinned_test.go`

- [ ] **Step 1: Test**

```go
func TestUserService_PinUnpin(t *testing.T) {
    stores, svcs := testStores(t)
    alice := seedUserSvc(t, stores, "alice")
    r1 := seedRepoSvc(t, stores, alice.ID, "one")
    r2 := seedRepoSvc(t, stores, alice.ID, "two")
    ctx := context.Background()

    if err := svcs.User.PinRepo(ctx, alice.ID, r1.ID); err != nil {
        t.Fatalf("pin r1: %v", err)
    }
    if err := svcs.User.PinRepo(ctx, alice.ID, r2.ID); err != nil {
        t.Fatalf("pin r2: %v", err)
    }
    ids, err := svcs.User.PinnedRepoIDs(ctx, alice.ID)
    if err != nil { t.Fatalf("list: %v", err) }
    if len(ids) != 2 || ids[0] != r1.ID || ids[1] != r2.ID {
        t.Errorf("expected order [r1, r2], got %+v", ids)
    }

    if err := svcs.User.UnpinRepo(ctx, alice.ID, r1.ID); err != nil {
        t.Fatalf("unpin: %v", err)
    }
    ids, _ = svcs.User.PinnedRepoIDs(ctx, alice.ID)
    if len(ids) != 1 || ids[0] != r2.ID {
        t.Errorf("expected only r2, got %+v", ids)
    }
}
```

- [ ] **Step 2: Implement**

```go
// in internal/store/user_store.go

// SetPinnedRepoIDs replaces the user's pinned-repo array.
func (s *UserStore) SetPinnedRepoIDs(ctx context.Context, userID int64, ids []int64) error {
    _, err := s.db.ExecContext(ctx,
        `UPDATE users SET pinned_repo_ids = $2 WHERE id = $1`, userID, pq.Array(ids))
    return err
}

func (s *UserStore) GetPinnedRepoIDs(ctx context.Context, userID int64) ([]int64, error) {
    var ids pq.Int64Array
    if err := s.db.GetContext(ctx, &ids, `SELECT pinned_repo_ids FROM users WHERE id = $1`, userID); err != nil {
        return nil, err
    }
    return []int64(ids), nil
}
```

```go
// in internal/service/user_service.go

func (s *UserService) PinRepo(ctx context.Context, userID, repoID int64) error {
    ids, err := s.store.GetPinnedRepoIDs(ctx, userID)
    if err != nil {
        return err
    }
    for _, id := range ids {
        if id == repoID {
            return nil
        }
    }
    if len(ids) >= 6 {
        return fmt.Errorf("pin limit reached (6)")
    }
    ids = append(ids, repoID)
    return s.store.SetPinnedRepoIDs(ctx, userID, ids)
}

func (s *UserService) UnpinRepo(ctx context.Context, userID, repoID int64) error {
    ids, err := s.store.GetPinnedRepoIDs(ctx, userID)
    if err != nil {
        return err
    }
    out := ids[:0]
    for _, id := range ids {
        if id != repoID {
            out = append(out, id)
        }
    }
    return s.store.SetPinnedRepoIDs(ctx, userID, out)
}

func (s *UserService) PinnedRepoIDs(ctx context.Context, userID int64) ([]int64, error) {
    return s.store.GetPinnedRepoIDs(ctx, userID)
}
```

(Import `github.com/lib/pq` for the array type if not already imported.)

- [ ] **Step 3: Routes for pin/unpin**

In `internal/router/router.go`:

```go
r.Post("/api/users/{id}/pinned-repos/{repoID}",   h.PinRepo)
r.Delete("/api/users/{id}/pinned-repos/{repoID}", h.UnpinRepo)
```

The handler verifies that `claims.UserID == id`.

- [ ] **Step 4: Run, verify, commit**

```bash
go test ./internal/service/ -run TestUserService_PinUnpin -v
git add internal/store/user_store.go internal/service/user_service.go internal/router/router.go internal/handler/
git commit -m "feat(service): add pin/unpin repo for users (max 6)"
```

---

### Task 4: `PinnedRepo` component

```go
// internal/view/components/pinned_repo.templ
package components

import "fmt"

type PinnedRepoData struct {
    OwnerName   string
    Name        string
    Description string
    Language    string
    LanguageColor string // hex
    Stars       int
}

templ PinnedRepo(d PinnedRepoData) {
    <a href={ templ.SafeURL("/" + d.OwnerName + "/" + d.Name) } class="block rounded-md border border-border bg-card p-4 hover:border-border-strong">
        <div class="flex items-center gap-2">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="text-muted-foreground"><path d="M9 12l2 2 4-4"/></svg>
            <span class="text-sm font-medium truncate">{ d.OwnerName + "/" + d.Name }</span>
        </div>
        if d.Description != "" {
            <p class="text-xs text-muted-foreground mt-2 line-clamp-2">{ d.Description }</p>
        }
        <div class="flex items-center gap-3 mt-2 text-[11px] text-muted-foreground">
            if d.Language != "" {
                <span class="flex items-center gap-1">
                    <span class="inline-block w-2 h-2 rounded-full" style={ "background-color: " + d.LanguageColor }></span>
                    { d.Language }
                </span>
            }
            <span>{ fmt.Sprintf("★ %d", d.Stars) }</span>
        </div>
    </a>
}
```

Test, regenerate, commit.

---

### Task 5.0: `LanguageService` (new) + `AggregateForUser` — TDD

This service does not exist today. It owns repo-language aggregation logic used by the profile page (and reusable by repo pages later).

**Files:**
- Create: `internal/service/language_service.go`
- Create: `internal/service/language_service_test.go`
- Modify: `internal/service/services.go` (wire into `Services` struct + `New()`)

- [ ] **Step 1: Define types and method signatures**

```go
type LangPercent struct {
    Name    string  // e.g. "Go"
    Color   string  // hex
    Percent float64 // 0..100
}

type LanguageService struct {
    repos *store.RepoStore
    cfg   config.GitConfig
}

// AggregateForUser sums language byte-counts across all of the user's
// owned repos (default branch HEAD) and returns the top `limit` as percentages.
func (s *LanguageService) AggregateForUser(ctx context.Context, userID int64, limit int) ([]LangPercent, error)

// Composition returns the language breakdown for a single ref of a single repo.
func (s *LanguageService) Composition(ctx context.Context, repoPath, ref string) (Composition, error)
```

- [ ] **Step 2: Test (table-driven)**

```go
func TestLanguageService_AggregateForUser(t *testing.T) {
    // seed two repos with one .go file and one .py file
    // assert top-2 result contains Go and Python with non-zero percent
}
```

- [ ] **Step 3: Implement**

Walk default-branch HEAD trees via go-git, classify by extension (linguist-style minimal table — `.go`, `.py`, `.js`, `.ts`, `.rs`, `.rb`, `.java`, `.c`, `.cpp`, `.cs`, `.css`, `.html`, `.md`, …). Sum blob sizes per language across all owned repos, sort desc, take top `limit`, compute percentages over the kept slice.

- [ ] **Step 4: Run, verify, commit**

```bash
go test ./internal/service/ -run TestLanguageService -v
git add internal/service/language_service.go internal/service/language_service_test.go internal/service/services.go
git commit -m "feat(service): add LanguageService with AggregateForUser"
```

---

### Task 5.1: Port `user.templ` overview tab to match `mockups/profile.html`

**Files:** Modify `internal/view/pages/user.templ`, handler, viewmodel.

**Mockup reconciliation (`mockups/profile.html`):**
- The mockup shows **followers / following counts** (lines 192–194) and **Edit profile / Share** buttons (lines 200–201). Cloudzilla has **no follow system**. **Drop** followers/following counts and the Share button from the port. Render the "Edit profile" link only when viewing own profile (it can point to `/settings/profile` which already exists). Document follow-system as future work in the self-review checklist.
- "Member since" date (mockup line 314) — surface `User.CreatedAt` (already on `model.User`) into the view model and render formatted (e.g. `Joined May 2025`).

- [ ] **Step 1: Extend `UserData`**

```go
type UserData struct {
    BasePage    view.BasePage
    User        *model.User
    IsOwnProfile bool
    Tab         string
    PinnedRepos []components.PinnedRepoData
    Heatmap     map[time.Time]int
    Activity    []components.ActivityRowData
    TopLangs    []service.LangPercent
    Orgs        []model.Organization
    MemberSince time.Time // = User.CreatedAt; surfaced for the sidebar
}
```

- [ ] **Step 2: Populate**

```go
ids, _ := h.Services.User.PinnedRepoIDs(ctx, user.ID)
for _, rid := range ids {
    r, err := h.Services.Repo.GetByID(ctx, rid)
    if err != nil { continue }

    // Primary language: prefer Repository.PrimaryLanguage (Phase 8) if present,
    // otherwise fall back to LanguageService.Composition(...).TopLanguage().
    lang := r.PrimaryLanguage
    if lang == "" {
        repoPath := filepath.Join(h.Cfg.Git.ReposRoot, r.OwnerName, r.Name+".git")
        if comp, err := h.Services.Language.Composition(ctx, repoPath, r.DefaultBranch); err == nil {
            lang = comp.TopLanguage()
        }
    }

    // Stars are not on model.Repository; query StarStore.
    stars, _ := h.Stores.Star.CountByRepo(ctx, r.ID)

    data.PinnedRepos = append(data.PinnedRepos, components.PinnedRepoData{
        OwnerName: r.OwnerName, Name: r.Name, Description: r.Description,
        Language: lang, LanguageColor: components.LangColor(lang),
        Stars: stars,
    })
}
data.Heatmap, _ = h.Services.CommitStats.LookbackForUser(ctx, user.ID, 365)
data.Activity, _ = h.Services.Event.UserActivity(ctx, user.Username, 1, 10) // takes username, not userID
data.Orgs, _    = h.Services.Org.ListForUser(ctx, user.ID)                  // owned + member (Task 6.1)
data.TopLangs, _ = h.Services.Language.AggregateForUser(ctx, user.ID, 5)
data.MemberSince = user.CreatedAt
```

> Note: `Repository.PrimaryLanguage` is conditional on Phase 8 adding the column. If Phase 8 does not, the fallback path keeps the page functional (at the cost of a tree walk per pinned repo on first render — acceptable for ≤6 pins).

- [ ] **Step 3: Body**

Renders inside `DashboardSubnav` chrome with `Active: "overview"`.

Header card: avatar (large) + name + bio + location + company + email link + "Member since {MemberSince}". When `IsOwnProfile`, render an "Edit profile" link to `/settings/profile`. Below, the "Overview" tab body:
- Pinned repos grid (max 6, each `@components.PinnedRepo(...)`)
- Contribution heatmap card (`@components.Heatmap(...)` with 52 weeks)
- Languages bar (`@components.LanguagesBar(...)`) driven by `data.TopLangs`
- Orgs strip
- Recent activity list (`@components.ActivityRow(...)`)

**Explicitly omitted from the port** (no backend support): follower/following counts, Follow button, Share button.

When `Tab != "overview"`, render the corresponding tab body (Repositories from Phase 8, Projects, Packages, Stars).

- [ ] **Step 4: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/user.templ internal/view/pages/user_templ.go internal/handler/ internal/service/ internal/view/view.go
git commit -m "feat(ui): port user profile overview tab to match mockup"
```

---

### Task 6.0: Add `OrgService.ListForUser` + `OrgService.CountMembers` — TDD

Both methods are referenced by Task 5.1 and Task 6.1 but **do not exist** today (`internal/service/org_service.go` only has `ListOwnedByUser`). Add them as TDD sub-tasks.

**Files:**
- Modify: `internal/store/org_store.go`
- Modify: `internal/service/org_service.go`
- Create: `internal/service/org_service_listforuser_test.go`

- [ ] **Step 1: Test**

```go
func TestOrgService_ListForUser_OwnedAndMember(t *testing.T) {
    stores, svcs := testStores(t)
    alice := seedUserSvc(t, stores, "alice")
    bob   := seedUserSvc(t, stores, "bob")
    ctx := context.Background()

    ownedOrg, _ := svcs.Org.Create(ctx, alice.ID, "alice-owned", "alice-owned", "")
    memberOrg, _ := svcs.Org.Create(ctx, bob.ID, "bob-owned", "bob-owned", "")
    _ = svcs.Org.AddMember(ctx, memberOrg.ID, bob.ID, alice.ID, model.OrgRoleMember)

    orgs, err := svcs.Org.ListForUser(ctx, alice.ID)
    if err != nil { t.Fatalf("ListForUser: %v", err) }
    if len(orgs) != 2 {
        t.Fatalf("want 2 orgs (owned + member), got %d", len(orgs))
    }
    // assert both surface
    names := map[string]bool{}
    for _, o := range orgs { names[o.Name] = true }
    if !names["alice-owned"] || !names["bob-owned"] {
        t.Errorf("missing org in result: %+v", orgs)
    }

    if c, err := svcs.Org.CountMembers(ctx, memberOrg.ID); err != nil || c != 2 {
        t.Errorf("CountMembers: got %d err=%v, want 2", c, err)
    }
}
```

- [ ] **Step 2: Implement store + service**

```go
// internal/store/org_store.go — new method.
// ListMembershipsForUser returns every org where the user has a row in org_members,
// regardless of role. Used by OrgService.ListForUser.
func (s *OrgStore) ListMembershipsForUser(ctx context.Context, userID int64) ([]model.Organization, error) {
    rows, err := s.db.QueryContext(ctx,
        `SELECT o.id, o.name, o.display_name, o.description, o.created_at, o.updated_at
         FROM organizations o
         JOIN org_members m ON m.org_id = o.id
         WHERE m.user_id = $1
         ORDER BY o.name`,
        userID)
    if err != nil { return nil, err }
    defer rows.Close()
    // scan into []model.Organization …
}

// CountMembers returns the total membership count for an org.
func (s *OrgStore) CountMembers(ctx context.Context, orgID int64) (int, error) {
    var c int
    err := s.db.QueryRowContext(ctx,
        `SELECT COUNT(*) FROM org_members WHERE org_id = $1`, orgID).Scan(&c)
    return c, err
}
```

Note: `OrgStore.ListByMember` already exists and may serve this purpose if it joins through `org_members` for any role. **Verify** before adding a new method — if `ListByMember` already returns owner+member rows, reuse it from the service layer.

```go
// internal/service/org_service.go
func (s *OrgService) ListForUser(ctx context.Context, userID int64) ([]model.Organization, error) {
    return s.orgs.ListMembershipsForUser(ctx, userID) // or s.orgs.ListByMember if it covers all roles
}

func (s *OrgService) CountMembers(ctx context.Context, orgID int64) (int, error) {
    return s.orgs.CountMembers(ctx, orgID)
}
```

- [ ] **Step 3: Run, verify, commit**

```bash
go test ./internal/service/ -run TestOrgService_ListForUser_OwnedAndMember -v
git add internal/store/org_store.go internal/service/org_service.go internal/service/org_service_listforuser_test.go
git commit -m "feat(service): add OrgService.ListForUser + CountMembers"
```

---

### Task 6.1: Port `org.templ` to match `mockups/organizations.html`

**Files:** Modify `internal/view/pages/org.templ`, handler.

**Mockup reconciliation (`mockups/organizations.html`):**
- The mockup has four list items — one Personal, two real orgs (Owner / Member), and one with a **"Collaborator"** role badge (line 245). Cloudzilla has no third "collaborator" org role; the only real roles are `owner` and `member` (`model.OrgRoleOwner`, `model.OrgRoleMember`). **Drop the Collaborator variant** from the port — only render `Owner` / `Member` badges.
- The mockup also shows the user's "Personal" account as the first row. Render this as a non-clickable header card or pass `nil` for `OrgWithRole.Org` when role == "personal"; document the choice.
- Page lives at `/settings/organizations` (account-settings chrome). The org **detail** page at `/{org}` is separate; this task targets the listing only. If the existing `org.templ` is a detail page, **split into `org_list.templ` + `org_detail.templ`** and route accordingly.

- [ ] **Step 1: Extend viewmodel**

```go
type OrgListData struct {
    BasePage view.BasePage
    UserOrgs []OrgWithRole
}

type OrgDetailData struct {
    BasePage view.BasePage
    Tab string
    Org *model.Organization
    Members []model.OrgMember
    Repos []model.Repository
}

type OrgWithRole struct {
    Org *model.Organization
    Role string // "owner" | "member"
    MemberCount int
}
```

- [ ] **Step 2: Populate listing**

```go
orgs, _ := h.Services.Org.ListForUser(ctx, claims.UserID)
for _, o := range orgs {
    role := "member"
    if h.Services.Org.IsOwner(ctx, o.ID, claims.UserID) {
        role = "owner"
    }
    count, _ := h.Services.Org.CountMembers(ctx, o.ID)
    data.UserOrgs = append(data.UserOrgs, OrgWithRole{Org: &o, Role: role, MemberCount: count})
}
```

(`OrgService.ListForUser` and `CountMembers` are introduced in **Task 6.0**.)

- [ ] **Step 3: Body for listing page**

Account-settings chrome (NOT `DashboardSubnav`). Title "Your organizations" + a list of `@orgListItem(...)` (defined locally in the templ file) showing org name, role badge (`Owner` / `Member` only — no Collaborator), member count, Leave button (members; never the last owner) or Manage button (owners). Top-right "New organization" CTA links to `/organizations/new`.

- [ ] **Step 4: Body for org detail page**

Header card: org logo + name + description + member count + repo count + "New repository" button (members + owners; route guarded server-side). Below: tabs (Repositories / People / Settings if owner). Default tab: Repositories grid.

- [ ] **Step 5: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/org.templ internal/view/pages/org_templ.go internal/handler/ internal/service/org_service.go
git commit -m "feat(ui): port org listing + detail pages to match mockups"
```

---

### Task 7: Create `new_organization.templ` + route

`/organizations/new` is **not currently routed**; this task registers it.

**Files:**
- Create: `internal/view/pages/new_organization.templ`
- Modify: `internal/router/router.go` (register routes + add to `pageNames`)
- Modify: `internal/handler/page_handler.go`
- Modify: `internal/handler/viewmodels.go`

**Mockup reconciliation (`mockups/new_organization.html`):**
- Has a `contact_email` field (line 192) — Cloudzilla's `organizations` table has no email column. **Drop the contact_email field** from the form to avoid expanding schema scope in a UI-port phase. (Future: add a column + migration if billing/contact-routing is built.)
- Has **Owner-type radios** with "My personal account" / "An enterprise account" (lines 198–211). Cloudzilla has no Enterprise concept. **Render owner-type radios as decorative / disabled** — show "My personal account ({username})" pre-selected and disable the Enterprise option with a `fg3` "Not available" note. Do not POST `owner_type`.
- Has **Plan radios** (Free / Team) (lines 215–230). Cloudzilla has no billing. **Render decorative**, mark `aria-disabled`, never POST `plan`.
- Has a **required TOS acknowledgement checkbox** (line 234). This **must** be present on the form (`required`) and enforced server-side (`if r.FormValue("accept_tos") != "on" { renderError }`).

- [ ] **Step 1: Register route**

In `internal/router/router.go` (and add `"new_organization"` to the `pageNames` slice per the new-feature checklist in CLAUDE.md):

```go
r.With(authMW).Get("/organizations/new",  h.PageNewOrganization)
r.With(authMW).Post("/organizations/new", h.CreateOrganization)
```

`OrgService.Create` already exists per `internal/service/org_service.go:27`.

- [ ] **Step 2: Handler**

```go
func (h *Handler) PageNewOrganization(w http.ResponseWriter, r *http.Request) {
    claims := authClaims(r)
    if claims == nil { http.Redirect(w, r, "/login", http.StatusFound); return }
    data := view.NewOrganizationData{BasePage: h.basePage(r)}
    h.render(w, r, pages.NewOrganization(data), "New organization")
}

func (h *Handler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
    claims := authClaims(r)
    if claims == nil { http.Redirect(w, r, "/login", http.StatusFound); return }

    if r.FormValue("accept_tos") != "on" {
        // re-render with error: TOS must be accepted
        return
    }
    name        := r.FormValue("name")
    description := r.FormValue("description")
    org, err := h.Services.Org.Create(r.Context(), claims.UserID, name, name, description)
    if err != nil { /* re-render with error */ return }
    http.Redirect(w, r, "/"+org.Name, http.StatusSeeOther)
}
```

- [ ] **Step 3: Template**

Standard global header (no subnav). Card with form fields:
1. Organization name (required, URL-safe; validated by `ValidateName` in service)
2. Description (optional)
3. Owner radios — decorative (personal pre-selected, Enterprise disabled with "Not available")
4. Plan radios — decorative (Free pre-selected, Team disabled with "Billing not yet supported")
5. TOS acknowledgement checkbox — required

Cancel link → `/settings/organizations`. Submit → `POST /organizations/new`.

- [ ] **Step 4: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/new_organization.templ internal/view/pages/new_organization_templ.go internal/handler/page_handler.go internal/handler/viewmodels.go internal/router/router.go internal/view/view.go
git commit -m "feat(ui): add new-organization page + route + create handler"
```

---

### Task 8: Port `repo_new.templ` to match `mockups/new_repo.html`

**Files:** Modify `internal/view/pages/repo_new.templ`, handler.

- [ ] **Step 1: Enumerate license + gitignore templates**

Add `RepoService.ListGitignoreTemplates()` and `ListLicenseTemplates()` returning hardcoded slices for now:

```go
var gitignoreTemplates = []string{"Go", "Node", "Python", "Rust", "Java", "C++", "Ruby"}
var licenseTemplates = []License{
    {Key: "mit", Name: "MIT License"},
    {Key: "apache-2.0", Name: "Apache License 2.0"},
    {Key: "gpl-3.0", Name: "GNU General Public License v3.0"},
    {Key: "bsl-1.1", Name: "Business Source License 1.1"},
}
```

- [ ] **Step 2: Body**

Owner selector (current user + orgs from `OrgService.ListForUser`), name input, description, visibility radios (Public / Private), init options: README checkbox, gitignore template select, license template select. Create button submits to existing `POST /api/repos` (or `/new`) endpoint.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/repo_new.templ internal/view/pages/repo_new_templ.go internal/handler/ internal/service/repo_service.go
git commit -m "feat(ui): port new-repo page (gitignore + license selectors)"
```

---

### Task 9: Verify and open PR

Tests + lint + templ regen + visual sweep on profile / orgs list / org detail / new_organization / new_repo in both themes. silent-failure-hunter. PR title: `feat(ui): UI overhaul phase 9 — profile & organizations`.

---

## Self-review checklist

- [ ] Migration **058** is additive (column with default) and survives an empty `pinned_repo_ids` correctly.
- [ ] Pin limit enforced (max 6 per profile); `UserService.PinRepo` is idempotent.
- [ ] `OrgService.ListForUser` returns both owned and member orgs (covered by `TestOrgService_ListForUser_OwnedAndMember`).
- [ ] `OrgService.CountMembers` returns the count of `org_members` rows for the org.
- [ ] `EventService.UserActivity(ctx, username, 1, 10)` is called with **username**, not user ID.
- [ ] `LanguageService.AggregateForUser` returns top-N as `[]LangPercent` summing across the user's owned repos.
- [ ] Profile primary-language path: prefers `Repository.PrimaryLanguage` when populated (Phase 8 column); falls back to `LanguageService.Composition(...).TopLanguage()`.
- [ ] Pinned-repo star counts are fetched via `StarStore.CountByRepo`, since `Repository` has no `StarCount` field.
- [ ] Profile page omits follower/following counts, the Follow button, and the Share button (no follow system in Cloudzilla). Documented as future work.
- [ ] Profile sidebar surfaces "Member since" from `User.CreatedAt`.
- [ ] Profile renders inside `DashboardSubnav` with `Active: "overview"`.
- [ ] Profile page handles a user with zero pinned repos / zero commits gracefully (heatmap renders empty grid).
- [ ] Organizations listing drops the mockup's "Collaborator" badge — only `Owner` / `Member` variants are rendered.
- [ ] Organizations listing lives under account-settings chrome at `/settings/organizations` (NOT `DashboardSubnav`).
- [ ] `/organizations/new` route is registered behind `authMW` and added to `pageNames` in `router.go`.
- [ ] `new_organization` form has owner-type and plan radios rendered as decorative/disabled and excludes the unsupported `contact_email` field.
- [ ] `new_organization` form requires the TOS checkbox client-side **and** server-side.
- [ ] New repository page enumerates orgs in the owner selector via `OrgService.ListForUser` when the user has them.
