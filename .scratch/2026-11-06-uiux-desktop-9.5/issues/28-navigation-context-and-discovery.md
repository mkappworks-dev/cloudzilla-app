# Navigation context, search peers and explore filters

Created: 2026-11-06
Category: enhancement
Status: needs-info
Blocked by: 14

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/navigation-context-and-discovery` | `.worktrees/feat+navigation-context-and-discovery` | `feat(ui): navigation context, search peers and explore filters` | polish (wave 1 re-score) | L | IA +0.4, Features +0.2, Content +0.1 |

## Problem

Found by the wave 1 re-score (Codex).

1. Stargazers drops the repository subnav and falls back to the account header (`stargazers.templ:12`, `:17`).
2. Code search is a separate route and form; the search result tabs do not list it as a peer (`search.templ:64`,
   `code_search.templ:18`).
3. Explore has no text search, language or topic filters, or pagination (`explore.templ:21`, `:46`).
4. Default branch in repo settings is a free-text field instead of a picker (`repo_settings.templ:181`; the file is
   ticket 14's, hence the dependency).
5. Password recovery without SMTP says "Ask an administrator" with no route to one, and the login page hides the
   recovery link in that configuration (`password_reset.templ:26`, `login.templ:68`).
6. "Cloudzilla Terms of Service" in the new-organization form has no page behind it. The wave 1 polish PR only removed
   the dead `#` link.

## Scope

Fix each so a user always knows where they are and can reach what the page promises. Item 6 needs a maintainer call:
write a terms page or drop the agreement.

## Files

- `internal/view/pages/stargazers.templ`, `search.templ`, `code_search.templ`, `explore.templ`,
  `repo_settings.templ`, `password_reset.templ`, `login.templ`, `new_organization.templ`
- matching `mockups/*.html`

## Acceptance criteria

- [ ] Stargazers shows the repository header and subnav
- [ ] Search offers repositories, code and the other kinds as peer tabs
- [ ] Explore filters by text and language and paginates
- [ ] Default branch is chosen from existing branches
- [ ] A no-SMTP instance tells the user how to recover access

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): needs-info for item 6 only (write a terms page or drop the agreement); items 1-5 are agent work and 14 has merged.

2026-11-12 (wave 2 re-score): Wave 2 re-score: (Codex) the sign-in page shows no "Forgot password?" link in the capture, and the reset page gives no way to reach an administrator (item 5, confirmed on `login`, `login-wrong-password`, `forgot-password`); general search has no Code tab beside its categories (item 2); (Claude) topic and starred-repository pages also drop their shell and sit on a narrower, offset grid (left edge 68px against the 44px gutter), as stargazers does (item 1).
