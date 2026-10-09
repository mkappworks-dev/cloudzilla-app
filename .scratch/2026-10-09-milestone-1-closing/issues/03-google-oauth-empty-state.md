# Google OAuth callback accepts an empty state

Created: 2026-10-09
Category: bug
Status: done

## Problem

The link and re-auth branches of the Google callback require a non-empty `oauth_state` cookie, but the main sign-in branch accepts an empty cookie when the `state` query parameter is also empty, then goes on to the token exchange.

If that is right, a victim who opens `/auth/google/callback?code=<attacker's code>` with no state cookie is signed in as the attacker (login CSRF). It would need a valid `code` for the attacker's own Google account, so impact is limited, but the check is cheap to make strict.

Read from the code only; not reproduced. The router tests could not get past the state check because `handler.UseFakeGoogle` is test-only inside the handler package.

## Where

`internal/handler/oauth_handler.go` `GoogleOAuthCallback` (~line 93)

## To decide

- Confirm by test that an empty cookie plus empty `state` currently proceeds. If it does not, close as `wontfix` with the reason.
- Expose the fake Google hook to router tests so the callback can be exercised past the state check (see ticket 08).

## Acceptance criteria

- [x] An empty `oauth_state` cookie, or a cookie that differs from `state`, is refused before any token exchange, in every branch.
- [x] Test covers: no cookie, empty cookie with empty state, mismatched state, matching state (still works).

## Blocked by

Nothing.

## Comments

Claude, 2026-10-09: Triaged as a bug. `TestGoogleOAuthRoutes_CallbackRefusesEmptyOrMismatchedStateBeforeExchange` failed on main: an `oauth_state` cookie with an empty value plus an empty or missing `state` returned 303 with a session cookie after three token exchanges. A missing cookie was already refused; only the present-but-empty one got through. The link and re-auth branches already required a non-empty cookie, and the test pins that. The main branch now does too, and `handler.UseFakeGoogle` is in `fake_google.go` so router tests can reach the callback (part of ticket 08).
