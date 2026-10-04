# CSRF middleware parses form bodies before any body limit

Created: 2026-10-03
Category: bug
Status: done

## Problem

`middleware.CSRF` is global (`r.Use` in `internal/router/router.go`). chi runs global middleware before routing, so CSRF runs before auth and before any route-level `middleware.MaxBodySize`. For a non-GET request with neither a Bearer token nor an `X-CSRF-Token` header, it calls `r.FormValue("csrf_token")`, and that parses the whole body:

- urlencoded: up to 10 MB in memory (net/http's own cap)
- multipart: values up to 42 MB in memory, and file parts spooled to temp files with no size limit

Two consequences:

1. Route body limits (`apiBodyLimit`, and the New file form's 26 MB limit) bind only Bearer and `X-CSRF-Token` requests. A cookie-authenticated plain HTML form post, which is what the browser sends, is parsed in full first. Verified on 2026-10-03 with a throwaway test: behind a 1 KB route limit, a multipart post carrying the CSRF cookie and a `csrf_token` field delivered a 64 KB field to the handler. The same post with a Bearer token was capped.
2. The parse runs before auth, on any path (including unknown ones), and whether or not the token matches. An anonymous client can stream a multipart body that the server spools to disk until `server.read_timeout` (15 s by default) ends the request, on as many connections as it opens. net/http deletes the temp files when the request ends.

The New file form still refuses an upload over 25 MB with 413 and caps the path in `CodeService.CommitFile`, so those protections don't depend on this fix.

## Fix sketch

- Cap form bodies ahead of `CSRF`: a global middleware that wraps `r.Body` in `http.MaxBytesReader` for `application/x-www-form-urlencoded` and `multipart/form-data` requests, sized for the largest legitimate form (`handler.MaxNewFileBodyBytes`). It has to be global, because a per-route limit runs after CSRF. Git transport bodies aren't form types, so pushes are unaffected.
- In `CSRF`, answer 413 when parsing hits that limit, rather than 403 "CSRF token mismatch", so an oversized browser upload gets a clear error.

## Acceptance criteria

- [x] A cookie-authenticated multipart post over the form cap is refused with 413, and nothing past the cap is read or spooled to disk.
- [x] An anonymous POST to any path can't make the server parse more than the form cap.
- [x] Route-level limits still apply to Bearer requests, and git pushes are unaffected.
