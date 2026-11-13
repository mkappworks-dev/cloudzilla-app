# Cross-repository (fork) pull requests

Created: 2026-11-13
Category: enhancement
Status: done

Decisions confirmed by the maintainer on 2026-11-13 (recommendations accepted as written).

## Problem Statement

A pull request can only be opened between two branches of the same repository. Daisy forks `mkappworks/cloudzilla`,
pushes `fix-x` to `daisy/cloudzilla`, and has no way to propose it: she needs write access to the upstream and has to
push her branch there, which defeats the point of a fork. Built-in CI has a fork-approval flow (`waiting` Runs, no
secrets, read-only token) that can never fire for the same reason.

## Solution

A pull request may name a head repository as well as a head branch. Daisy opens the PR from `daisy/cloudzilla:fix-x`
into `mkappworks/cloudzilla:main`; the PR lives in the base repository, shows `daisy:fix-x` as its head, follows her
pushes to the fork, and merges into the base like any other. The base repository's reviewers need no access to the fork.
CI treats it as a fork Run: it waits for a writer's approval and runs without secrets.

## How pull requests work today

- A pull request is one `pull_requests` row with `repo_id`, `head_branch`, `base_branch` and `head_sha`. There is no head
  repository: `PullService.Create` takes one `(owner, repo)` and two branch names.
- Every read and merge path (`Mergeability`, `PullCommits`, `GetPullDiff`, `MergePullRequest`,
  `ThreeWayMergePullRequest`, `SquashMergePullRequest`, `UpdatePullBranch`) opens one repository's object store and
  resolves both branches in it. [pr-merge](../../docs/pr-merge.md) describes the merge methods.
- A push updates head SHAs in `OnPostReceive` with `UpdateHeadSHAByBranch(repo.ID, branch, sha)`: it matches PRs by the
  pushed repository's own id, so a push to a fork never reaches a PR stored in the upstream.
- Forks exist: `repositories.fork_of_id` (`ON DELETE SET NULL`) points at the source, and `RepoService.Fork` copies the
  whole git directory, so a fork starts with the source's full history.

## Decisions

1. **Which repositories can be a head.** Any repository in the same fork tree as the base: the base itself, a fork of
   it, a fork of a fork, a sibling fork, or the source it was forked from. Two repositories are in one tree when
   following `fork_of_id` from each reaches the same root. Unrelated repositories are refused (`422`). When an upstream
   is deleted its forks lose `fork_of_id` and leave the tree; accepted.
2. **Objects are copied into the base.** On create and on every push to the head branch, the head commit and its missing
   objects are copied into the base repository's object store and `refs/pull/<number>/head` in the base points at the
   commit. Diffs, mergeability, the commit list, all three merge methods and CODEOWNERS then run against one repository,
   as today, with the PR's head ref being the branch name for a same-repo PR and `refs/pull/<number>/head` for a
   cross-repo one. Same-repo PRs get no ref and do not change. Git transport does not advertise `refs/pull/*` to clones
   or fetches by default.
3. **Who can open one.** `CanRead` on the base repository, `CanWrite` on the head repository, and the head branch must
   exist there. Daisy needs no access to the base beyond reading it. A mirror base is refused as today.
4. **The base's readers need nothing on the head.** A reader of the base who cannot read the head repository sees the PR,
   its diff and commits (the objects are in the base), and the head labelled "a fork" instead of `daisy:fix-x`.
5. **Maintainers cannot push to the PR branch in v1.** No "allow edits from maintainers". **Update branch** merges the base
   into the head and so would write to the fork: it is not offered on cross-repo PRs, and the merge box says to update
   from the fork. A per-PR opt-in is a later spec.
6. **Merging writes the base only.** The head repository is never written. **Delete branch after merge** is offered for
   same-repo PRs only.
7. **Head repository deleted** (soft or hard): its open PRs are closed with a timeline event ("head repository was
   deleted"), as Gitea does. A merged PR keeps its recorded SHAs and its commits, which live in the base. The PR stores a
   snapshot of the head's `owner/name` so the page can still say where it came from.
8. **Head branch deleted** in the head repository: the PR is closed with a timeline event. A force-push is a normal head
   update: the ref is moved and `head_sha` updated.
9. **Head repository renamed or transferred:** the PR keeps working, because it stores the head repository's id
   ([repo-rename](../../docs/repo-rename.md)); the displayed name is read from the repository, not the snapshot.
10. **Head repository becomes private:** open PRs stay open and keep working; only the label changes for readers who can no
    longer read it (decision 4).
11. **Webhooks and notifications** fire on the base repository only; a push to the fork fires the fork's own `push`
    webhooks as today and no `pull_request` event on the fork. The `pull_request` payload gains the head repository
    (`id`, `full_name`, `fork`), and notifications link to the base PR.
12. **Number and listing.** The PR number comes from the base's sequence, and every list, search qualifier and API field
    that shows a head branch shows `owner:branch` when the head differs.
13. **CI** (last ticket, after `feat/phase-18-ci-pipelines` merges): `WorkflowRunService.OnPullRequest` is called with the
    head repository, a cross-repo Run is `waiting` until a writer approves, and its per-job token is bound to the head
    repository so the runner can clone the fork's commit. The commit statuses a fork Run posts attach to the PR's head
    SHA in the base. The fork limit in `docs/ci.md` is removed.

## User Stories

1. As a contributor with a fork, I want to open a pull request from my fork's branch into the upstream, so that I can
   propose a change without write access to it.
2. As a contributor, I want the new-PR form to let me choose the head repository (my fork, or any fork in the tree I can
   write to) and its branch, so that I open the PR I mean.
3. As a contributor, I want the compare page to default the head repository to my fork of this repository, so that the
   usual case is one click.
4. As a contributor, I want my later pushes to the fork branch to update the open PR, so that I do not reopen it.
5. As a contributor, I want a force-push to the fork branch to update the PR, so that I can rewrite my history.
6. As a maintainer, I want the PR to show `daisy:fix-x` as its head and link to the fork's branch when I can read it, so
   that I know where it came from.
7. As a maintainer, I want to review the diff, the commits and the files of a fork PR exactly as for a same-repo PR, so
   that review does not change.
8. As a maintainer, I want fast-forward, merge-commit and squash to work on a fork PR and write only to my repository,
   so that merging a contribution never touches the contributor's fork.
9. As a maintainer, I want conflict detection to work for a fork PR, so that I am not offered a merge that fails.
10. As a maintainer, I want required reviews and required checks on the base branch to apply to fork PRs, so that a fork is
    no way around branch protection.
11. As a maintainer, I want a fork PR whose head SHA moved to lose its approvals the same way a same-repo PR does, so that I
    never merge unreviewed code.
12. As a maintainer, I want **Update branch** hidden on a fork PR with a short explanation, so that I am not offered a
    button that cannot work.
13. As a maintainer, I want CI for a fork PR to wait for my approval and run without secrets, so that a stranger's workflow
    cannot read them.
14. As a reader of the base who cannot read the fork, I want to see the PR and its diff with the head shown as "a fork", so
    that private forks are not exposed by name.
15. As a contributor, I want my PR closed with a clear event when I delete my fork or its branch, so that nobody waits on a
    PR that cannot update.
16. As a maintainer, I want a merged fork PR to keep its Commits and Files tabs after the fork is deleted, so that history
    is not lost.
17. As a contributor, I want a rename or transfer of my fork not to break my open PRs, so that housekeeping does not cost me
    reviews.
18. As an API or `cz` user, I want to create a PR with a head repository, so that scripts can open fork PRs.
19. As an API user, I want the PR JSON to say which repository its head is in, so that tools can tell a fork PR apart.
20. As a subscriber, I want notifications and webhooks for a fork PR on the base only, so that I get one stream per PR.
21. As a maintainer of an archived or mirrored base, I want fork PRs refused the same way as same-repo PRs.
22. As an instance admin, I want PRs between unrelated repositories refused, so that the head picker cannot be used to pull
    in an arbitrary repository's objects.
23. As a maintainer, I want fork PRs listed and filterable with the others, so that my PR list is one list.
24. As a contributor, I want a fork PR draft to behave like any draft, so that I can mark it ready when done.
25. As a maintainer, I want the PR's "closes #N" keywords to work on merge of a fork PR, so that issues close.

## Implementation Decisions

- **Schema:** `pull_requests.head_repo_id` (nullable `REFERENCES repositories ON DELETE SET NULL`; existing rows backfilled
  with `repo_id`; NULL after the head repository is hard-deleted) and `head_repo_label` (text snapshot of `owner/name`,
  set at create and refreshed on a head update). The next sequential migration; re-check the number after merging main.
- **Head ref seam:** one function derives a PR's head ref (branch name, or `refs/pull/<n>/head`). Every `CodeService` pull
  method already takes a head string, so the change is in the callers and in `resolveHead`, which must accept a full ref.
  The merge methods' "head moved since checked" guard compares the ref's current SHA, as today.
- **Object copy:** a `CodeService` method that fetches one commit's reachable objects from the head repository's store
  into the base's and sets the PR ref atomically, pure go-git (no `git` binary). It is called on create and on push;
  failures on push are logged and leave the old ref, with the PR showing the old SHA, never a broken state.
- **Push sync:** `OnPostReceive` also finds open PRs by `head_repo_id` + `head_branch` (any base repository), runs the
  copy, updates `head_sha`, and keeps updating same-repo PRs as today. A branch delete closes the matching PRs (decision 8).
  Whatever else a same-repo head update triggers (review dismissal, CI, auto-merge re-evaluation) runs for cross-repo
  updates too.
- **Repo delete:** the soft and hard delete paths close open PRs whose head is the repository (decision 7) before the row
  goes.
- **Permissions:** `PullService.Create` takes the head repository and checks decision 3. Reading a PR is `CanRead` on the
  base only. `repoSvc` fork-tree membership is a store query on `fork_of_id` chains.
- **UI:** the new-PR page gains a head-repository picker (the viewer's writable repositories in the tree) before the
  branch picker, and the PR header, list rows, merge box and timeline show `owner:branch` and the decision-4 fallback.
  Part of the same PR as the pages: the standing mocks in `mockups/` (new PR, detail) and `mockups/COVERAGE.md`.
- **API and CLI:** `POST /api/repos/{owner}/{repo}/pulls` accepts an optional `head_repo` (`owner/name`); the PR JSON gains
  `head_repo` (`owner/name`, absent when same). `cz pr create` gains `--head-repo`. Update [api-reference](../../docs/api-reference.md)
  and [cli](../../docs/cli.md).
- **Webhooks and notifications:** decision 11; the existing `pull_request` payload keeps `head_branch` and gains
  `head_repo`.
- **Docs:** [pr-merge](../../docs/pr-merge.md) (head ref, why Update branch is hidden), [git-transport](../../docs/git-transport.md)
  (`refs/pull/*` not advertised, push sync), [webhooks](../../docs/webhooks.md), [api-reference](../../docs/api-reference.md),
  [ci](../../docs/ci.md) (last ticket), [ROADMAP](../../docs/ROADMAP.md).

## Testing Decisions

- **Seam:** the existing highest seam, HTTP handler and router tests over a real database and real go-git repositories
  (`newVerificationRouter`, `internal/handler` pull tests, the fork tests in `internal/router/fork_test.go`): fork a
  repository, push a commit to the fork, open the PR through the API, then diff, merge and push again, asserting on
  responses and the base repository's refs. Service-level tests with real repositories (the `seedRepoWithCODEOWNERS`
  style) for the push sync and the delete cases, since a git push is not an HTTP call.
- A good test asserts external behaviour: status codes, the PR JSON, the base repository's branch tip after a merge, the
  head repository left untouched, and who sees what. It does not assert on which ref name a helper chose.
- **Cases:** each merge method, a conflict, force-push, a branch delete, a repository delete, a rename, a private fork
  viewed by a base-only reader, a sibling fork, an unrelated repository (refused), a writer-less opener (refused), and
  Update branch hidden.
- **Prior art:** `internal/router/fork_test.go`, `internal/service/repo_fork_test.go`, `code_service_merge_test.go`,
  `pull_service_ci_test.go`, the throwaway-server seeding notes for live checks (light and dark at 1440 for the UI).

## Out of Scope

- Allow edits from maintainers, and pushing to a fork's branch from the PR page.
- PRs between repositories outside one fork tree, and AGit-style `refs/for/*` pushes.
- Cross-repo PRs that point at a head repository on another Cloudzilla instance.
- Rebase merges (the app has none).
- Changing how forks are created.

## Further Notes

- Gitea fetches the head into the base and owns `refs/pull/N/head` there, and closes cross-repo PRs when the head
  repository is deleted; GitHub shares one object store across the fork network; GitLab documents merge requests only from
  a fork to its upstream. This spec follows Gitea's model with GitHub's fork-tree scope. Several cells of that comparison
  were unverified (the report is in the session, not committed); the unverified ones are not relied on here.
- Disk use: the base holds a copy of every fork PR's new objects. Accepted; `RepoService` quota recompute already runs
  after a push, and the copy should be counted against the base.
- The CI ticket is blocked until `feat/phase-18-ci-pipelines` is merged. The spec and the other tickets do not depend on it.
- Accepted after review: the `pull_request` webhook payload and a default merge message name the head repository even
  when it is private, because the base's maintainers see the pull request and its author chose to open it; only the API
  and the pages withhold the name from a reader of the base who can't read the fork. The head picker runs one role
  lookup per repository in the network (at most 200) on each new-PR page load; batching them is a later change if a
  network grows that large.
