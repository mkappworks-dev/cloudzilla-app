# Auto-subscribe on participation

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 01

## What

Call `AutoSubscribe` from: `IssueService.Create`, `PullService.Create` and `DiscussionService.Create` (reason `author`); `CommentService.CreateForIssue`, `CreateForPull` and `DiscussionService.CreateReply` (`comment`); the PR review submit path (`review`); `AssigneeService.AddToIssue` and `AddToPull` (`assign`, subscribing the assignee, not the actor). Mentions are handled in ticket 02.

Failures are logged and never fail the request.

## Acceptance criteria

- [ ] Each action above creates a `subscribed` row with the right reason for the right user.
- [ ] A second action on the same thread keeps the first reason.
- [ ] Acting on a thread the user has muted leaves it muted.
- [ ] A failing subscription write doesn't fail the request (test with a stubbed error).
