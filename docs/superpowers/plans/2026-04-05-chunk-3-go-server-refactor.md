# Milestone 1 — Chunk 3: Go Server Refactor + Professional Comments ✅ COMPLETE

> **Scope:** Out of scope for `tech/m1-licensing-security-cleanup`. Implement on a separate branch (e.g., `tech/chunk-3-go-server-refactor`).

**Goal:** Split three oversized Go files into focused, domain-specific files and add GoDoc comments to all exported functions/types across the codebase.

---

## File Map

| File | Lines | Action |
|------|-------|--------|
| `internal/service/code_service.go` | 1,927 | Split into 8 files |
| `internal/handler/page_handler.go` | 1,119 | Split into 7 files |
| `internal/view/viewmodels.go` | 855 | Split into 9 files |
| `internal/service/sso_service.go` | 939 | Keep as-is (cohesive) |
| `internal/store/repo_store.go` | 665 | Keep as-is (single entity) |
| `internal/router/router.go` | 425 | Keep as-is (single purpose) |

---

## Task 1: Split `internal/service/code_service.go` into 8 Files

All methods share the `*CodeService` receiver. All new files are `package service`. No import path changes.

### File 1: `code_service.go` (keep, ~140 lines) — Core struct, constructor, helpers

Keep:
- `var ErrEmptyRepo`, `var ErrRefNotFound`
- Types: `BranchInfo`, `TagInfo`, `RefsResult`, `BreadcrumbPart`, `CodeLine`
- `CodeService` struct + `NewCodeService`
- `repoPath()`, `wikiPath()`
- `resolveRef()`, `ResolveRef()`
- `buildBreadcrumbs()`

### File 2: `code_service_wiki.go` (~200 lines) — Wiki operations

Move: `WikiPageList`, `WikiPageGet`, `WikiPageSave`, `WikiPageDelete`, `wikiCommit`, `wikiDelete`

### File 3: `code_service_tree.go` (~150 lines) — Tree/blob browsing

Move: Types `TreeEntry`, `TreeResult`, `BlobResult` + `GetTree`, `GetBlob`, `GetRawBlob`, `GetProfileReadme`

### File 4: `code_service_commit.go` (~200 lines) — Commit history and detail

Move: Types `CommitSummary`, `CommitLog`, `DiffLine`, `DiffHunk`, `FileDiff`, `CommitDetail` + `GetCommits`, `GetCommit`, `buildHunks`

### File 5: `code_service_blame.go` (~60 lines) — Blame

Move: Types `BlameLine`, `BlameResult` + `GetBlame`

### File 6: `code_service_merge.go` (~450 lines) — PR diff and merge

Move: Types `PRDiffResult`, `mergeFile` + `checkFastForward`, `GetPullDiff`, `MergePullRequest`, `flattenTree`, `findMergeBase`, `buildTree`, `mergeTreesNoConflict`, `ThreeWayMergePullRequest`, `SquashMergePullRequest`, `ApplySuggestion`

### File 7: `code_service_refs.go` (~200 lines) — Branch/tag management, templates, codeowners

Move: `ListRefs`, `CreateBranch`, `DeleteBranch`, `CreateTag`, `DeleteTag`, `GetIssueTemplates`, `GetPRTemplate`, Types `IssueTemplate` + `templateName`, `GetCodeOwners`, `MatchCodeOwners`

### File 8: `code_service_insights.go` (~120 lines) — Contributor/activity stats

Move: Types `ContributorStat`, `WeeklyActivity`, `CodeFrequencyWeek` + `weekStart`, `GetContributors`, `GetCommitActivity`, `GetCodeFrequency`

**Verification:** `go build ./...` and `go test ./...` must pass after each file move.

---

## Task 2: Split `internal/handler/page_handler.go` into 7 Files

All methods are on `*Handler`. The `basePage()` helper stays in the root file.

### File 1: `page_handler.go` (keep, ~50 lines)

Keep: `basePage()`, `PageHome`

### File 2: `page_auth_handler.go` (~130 lines)

Move: `PageLogin`, `PageLoginSubmit`

### File 3: `page_user_handler.go` (~100 lines)

Move: `PageUser`, `pageOrgProfile`, `PageOrgSettings`

### File 4: `page_repo_handler.go` (~200 lines)

Move: `PageRepo`, `PageRepoSettings`, `PageRefs`, `PageTree`, `PageBlob`, `PageCommits`, `PageCommit`, `PageBlame`

### File 5: `page_issue_handler.go` (~200 lines)

Move: `PageIssues`, `PageIssueDetail`, `PageNewIssue`, `PageNewIssueSubmit`

### File 6: `page_pull_handler.go` (~250 lines)

Move: `PagePulls`, `PageNewPull`, `PageNewPullSubmit`, `PagePullDetail`

### File 7: `page_settings_handler.go` (~50 lines)

Move: `PageSettings`, `PageNotifications`

**Verification:** `go build ./...` must pass after each file move.

---

## Task 3: Split `internal/view/viewmodels.go` into 9 Files

All types are in `package view`. No methods — pure struct declarations. Splits mirror the handler split.

### File 1: `viewmodels.go` (keep, ~50 lines) — Base types

Keep: `RenderedComment`, `RenderedLineComment`, `BasePage`, `HomeData`, `FeedData`

### File 2: `viewmodels_auth.go` (~30 lines)

Move: `LoginData`, `SetupData`, `InviteData`, `SecurityPageData`, `TOTPVerifyPageData`

### File 3: `viewmodels_user.go` (~50 lines)

Move: `UserData`, `OrgData`, `OrgSettingsData`, `OrgMembersFragData`, `UserStarsData`, `UserGistsData`

### File 4: `viewmodels_repo.go` (~120 lines)

Move: `RepoData`, `ReleasesData`, `ReleaseDetailData`, `RepoSettingsData`, `BranchProtectionsFragData`, `DeployKeysFragData`, `WebhooksFragData`, `WebhookDeliveriesFragData`, `RefsData`, `BranchesFragData`, `TagsFragData`, `TreeData`, `BlobData`, `BlameData`, `CommitsData`, `CommitData`, `RepoCollaboratorsFragData`, `RepoLabelsFragData`, `ForkButtonData`, `StarButtonData`, `WatchButtonData`, `RepoTopicsFragData`

### File 5: `viewmodels_issue.go` (~80 lines)

Move: `IssuesData`, `IssueDetailData`, `IssueNewData`, `IssueDetailFragData`, `IssueLabelSidebarData`, `IssueAssigneeSidebarData`, `MilestonesData`, `MilestonesListFragData`, `MilestoneSidebarFragData`

### File 6: `viewmodels_pull.go` (~80 lines)

Move: `PullsData`, `PullDetailData`, `PullNewData`, `PRReviewsFragData`, `PullDetailFragData`, `PullLabelSidebarData`, `PullAssigneeSidebarData`, `LineCommentsFragData`, `LineCommentFormFragData`

### File 7: `viewmodels_comment.go` (~20 lines)

Move: `CommentFragData`, `CommentsFragData`, `ReactionFragData`, `SavedRepliesData`, `SavedRepliesFragData`, `SavedRepliesPickerFragData`

### File 8: `viewmodels_settings.go` (~80 lines)

Move: `SettingsData`, `NotificationsData`, `NotificationsFragData`, `NotificationItemFragData`, `AdminSettingsData`, `AdminSettingsFragData`, `AdminInvitationsFragData`, `SSHKeysFragData`, `TokensData`, `TokensListFragData`, `AuditLogData`, `SSOSettingsData`, `NotificationSettingsData`, `OAuthAuthorizeData`, `OAuthAppsData`

### File 9: `viewmodels_social.go` (~100 lines)

Move: `RenderedDiscussionReply`, `DiscussionsData`, `DiscussionDetailData`, `GistsData`, `GistDetailData`, `GistNewData`, `GistEditData`, `StargazersData`, `SearchData`, `TopicData`, `CodeSearchData`, `ExploreData`, `DependenciesData`, `ProjectsData`, `ProjectDetailData`, `WikiPageData`, `WikiEditData`, `PulseData`, `ContributorsData`

**Note:** `internal/handler/viewmodels.go` type aliases need no changes — Go resolves types at package level, not file level.

---

## Task 4: Add GoDoc Comments

Systematic pass across the entire codebase. GoDoc conventions:
- Start with the function/type name: `// GetTree returns the directory listing...`
- Be concise — one or two sentences
- Explain purpose, not implementation
- Document error return semantics where relevant

### Phase 4a: Service layer (`internal/service/`)
All exported functions and types, prioritizing newly split `code_service_*.go` files.

### Phase 4b: Handler layer (`internal/handler/`)
Every exported handler method — describe the route it serves and what it renders/returns.

### Phase 4c: Store layer (`internal/store/`)
Every exported function and type.

### Phase 4d: View models (`internal/view/`)
Every exported type.

### Phase 4e: Other packages
`internal/model/`, `internal/middleware/`, `internal/config/`, `internal/router/`, `internal/ssh/`, `internal/markdown/`

---

## Implementation Sequence

1. Create branch `tech/chunk-3-go-server-refactor` from main
2. Split `code_service.go` → verify `go build ./...` and `go test ./...`
3. Split `page_handler.go` → verify compilation
4. Split `viewmodels.go` → verify compilation
5. Add GoDoc comments (service → handler → store → view → other)
6. Run `go vet ./...` and `go test ./...`
7. Commit

---

## Risk Mitigation

- **All splits within same Go package** — no import paths change, no external consumer breakage
- **Handler viewmodels alias file** needs no changes (package-level resolution)
- **Existing tests** (`code_service_profile_readme_test.go`) continue to work (same package)
- **No circular imports** possible (intra-package splits)
