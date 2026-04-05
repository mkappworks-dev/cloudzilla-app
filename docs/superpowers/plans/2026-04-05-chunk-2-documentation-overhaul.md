# Milestone 1 — Chunk 2: Documentation Overhaul ⚠️ PARTIAL

> **Status:** Tasks 3-5 done on `tech/m1-licensing-security-cleanup`. Tasks 1-2 (doc extraction) still pending.

**Goal:** Slim the README from 1,292 → ~200 lines by extracting reference material into docs/, add milestone labels to the roadmap, and fix stale references in CLAUDE.md.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `docs/api-reference.md` | Create | Full API endpoint tables extracted from README |
| `docs/configuration.md` | Create | Config reference, env vars, CLI, production config |
| `README.md` | Rewrite | Slim to ~200 lines with links to docs/ |
| `docs/ROADMAP.md` | Edit | Add Milestone 1/2 headers |
| `CLAUDE.md` | Edit | Add milestone structure, fix stale Templ references |

---

## Task 1: Create `docs/api-reference.md` ⏳ PENDING

- [ ] Extract the entire "API Reference" section from README (17 subsections of endpoint tables)
- [ ] Straight extraction — no content changes needed
- [ ] Subsections: Auth, SSH Keys, PATs, Deploy Keys, Users, Repos, Issues, Labels, Assignees, Stars, Forks, PRs, PR Reviews, PR Line Comments, Branch Protections, Search, Releases, Commit Statuses, Milestones, Branches & Tags, Git Ops, Instance Admin, Setup & Invitations, Collaborators, Orgs, Webhooks, Notifications, HTMX Fragments

---

## Task 2: Create `docs/configuration.md` ⏳ PENDING

- [ ] Merge content from multiple README sections into one file:
  - Configuration Reference table (key/default/description)
  - Docker env var overrides and SMTP settings
  - Production config.yaml example
  - SSH host key setup
  - CLI Reference (`cloudzilla migrate` commands)

---

## Task 3: Slim down `README.md` to ~200 lines ✅ DONE

**Sections to KEEP (with trimming):**

| Section | Target Lines | Notes |
|---------|-------------|-------|
| Title + description + badges | ~10 | Add license badge, milestone status |
| Architecture Overview (mermaid) | ~55 | Main diagram only |
| Features | ~30 | Condense to category bullets |
| Quick Start (Docker) | ~20 | Keep as-is |
| Quick Start (Local) | ~30 | Trim config to essentials |
| Make Targets | ~20 | Keep the table |
| Project Structure | ~25 | Keep tree, drop architecture layers |
| Documentation links | ~15 | New section linking to all docs/ |
| License | ~8 | BSL 1.1 summary (from Chunk 1) |

**Sections to REMOVE (replaced with links to docs/):**

- Request/Auth/Git Transport/Permission flow diagrams → link to respective docs
- Docker & Production Deployment → `docs/deployment.md`
- Configuration & CLI Reference → `docs/configuration.md`
- SSH Keys & Git Operations → `docs/git-transport.md`
- Permission Model → `docs/access-control.md`
- Code Browser → `docs/code-browser.md`
- API Reference → `docs/api-reference.md`
- Roadmap full table → 3-line status summary + link to `docs/ROADMAP.md`
- Database Migrations → remove (internal detail)
- Dependencies table → remove (in go.mod)

**New "Documentation" section:**

```markdown
## Documentation

| Topic | Link |
|-------|------|
| API Reference | [docs/api-reference.md](docs/api-reference.md) |
| Configuration & CLI | [docs/configuration.md](docs/configuration.md) |
| Deployment | [docs/deployment.md](docs/deployment.md) |
| Access Control | [docs/access-control.md](docs/access-control.md) |
| Git Transport | [docs/git-transport.md](docs/git-transport.md) |
| Code Browser | [docs/code-browser.md](docs/code-browser.md) |
| HTMX Patterns | [docs/htmx-patterns.md](docs/htmx-patterns.md) |
| PR Merging | [docs/pr-merge.md](docs/pr-merge.md) |
| Organizations | [docs/organizations.md](docs/organizations.md) |
| Webhooks | [docs/webhooks.md](docs/webhooks.md) |
| Notifications | [docs/notifications.md](docs/notifications.md) |
| Full Roadmap | [docs/ROADMAP.md](docs/ROADMAP.md) |
```

---

## Task 4: Update `docs/ROADMAP.md` ✅ DONE

- [x] Add **Milestone 1 header** before Phase 0:
  ```
  # Milestone 1 — Core Platform (Phases 0-15.3) ✅ COMPLETE
  All 53 migrations implemented. Cleanup and review in progress.
  ```

- [x] Add **Milestone 2 header** before Phase 16:
  ```
  # Milestone 2 — Advanced Infrastructure (Phases 16-20) ⬜ PLANNED
  Container registry, Git LFS, CI/CD, clustering, and GraphQL API.
  ```

- [x] Add closing note after Phase 15.3 content:
  > Milestone 1 Closing (April 2026): All phases code-complete. Current work: BSL 1.1 license, documentation overhaul, testing, final review before v0.1.0.

---

## Task 5: Update `CLAUDE.md` ✅ DONE

- [x] **Add milestone structure** after the roadmap table:
  ```
  ### Milestone Structure
  - Milestone 1 (Phases 0-15.3, migrations 001-053): Core platform — code complete
  - Milestone 2 (Phases 16-20, migrations 054-060+): Advanced infrastructure — planned
  ```

- [x] **Fix stale Templ references** (from pre-migration html/template era):
  - "Adding a New Feature" step 8: change `cmd/server/frontend/templates/` → `internal/view/pages/`
  - Step 10: update fragment reference to `internal/view/fragments/`
  - HTMX & Template Patterns section: update "Templates parsed at startup in `router.mustParseTemplates()`" to reflect Templ compilation model
  - Verify `pageNames` slice reference in step 11 still exists

- [x] **Verify all referenced paths** still exist (internal/*, cmd/*, docs/*, etc.)

- [ ] **Add references** for new docs: `docs/api-reference.md`, `docs/configuration.md` (blocked on Tasks 1-2)

---

## Implementation Sequence

```
[1] docs/api-reference.md ─┐
                            ├─► [3] README.md rewrite ─┐
[2] docs/configuration.md ─┘                           ├─► [5] CLAUDE.md update
                            [4] ROADMAP.md update ─────┘
```

Steps 1+2 in parallel → Steps 3+4 in parallel → Step 5 last.

---

## Potential Challenges

1. **Templ migration staleness in CLAUDE.md:** The feature checklist and HTMX patterns reference old `html/template`. Must read `internal/view/` and `internal/router/router.go` to confirm current state.

2. **README features condensation:** Current 50-line bullet list needs grouping into ~30 lines (e.g., "PR workflow: reviews, line comments, suggestions, drafts, auto-merge, branch protection, CODEOWNERS").

3. **Deployment content overlap:** Verify `docs/deployment.md` already has all README deployment content before removing. Merge missing details if needed.

4. **License badge:** Works once Chunk 1 lands the LICENSE file. Add badge regardless.
