# Tailwind's watcher ignores the @source glob, so new classes emit no CSS

Created: 2026-10-11
Category: bug
Status: needs-triage

## Problem

`make dev` runs `bin/tailwindcss -i tailwind/input.css -o cmd/server/frontend/static/main.css --watch=always`.
That watcher reacts to its own entry file but not to the files it scans:

- Appending a rule to `tailwind/input.css` → `main.css` is rewritten in **0.5s**.
- Adding `class="rotate-[17deg]"` to `internal/view/pages/setup.templ`, where `17deg` provably
  did not occur in `main.css` → **30s later `main.css` had not been touched at all** (same mtime,
  same hash).

`tailwind/input.css` declares `@import "tailwindcss" source(none);` with an explicit
`@source "../internal/view/**/*.templ"`, and that glob resolves correctly — a one-shot
`make build-css` does pick the class up. It is only watch mode that never rescans it.

The effect on a developer: the markup reloads with the new class attached and no rule behind it,
so the element renders unstyled and the cause is invisible. Classes already used anywhere in the
project are unaffected, because they are already in `main.css` — which is why this goes unnoticed
until someone reaches for a utility the project has not used before.

This predates the hot-reload work: the Tailwind invocation in `make dev` is unchanged from before
that branch. It was found while verifying it.

## Notes

Worth checking before fixing:

- Whether Tailwind v4.3.3 watches `@source` globs at all under `source(none)`, or whether the
  glob needs expressing differently (an `@source` directory rather than a glob, say).
- Whether a newer Tailwind fixes it — v4.3.3 is pinned in the `Makefile` as `TAILWIND_VERSION`.
- Failing both, `make dev` could watch `internal/view/**/*.templ` itself and re-run `build-css`,
  which is what air already does for Go files.

## Acceptance criteria

- [ ] Adding a never-before-used Tailwind class to a `.templ` file results in that class's CSS
      appearing in `main.css` while `make dev` is running, with no manual command
- [ ] The CONTRIBUTING.md caveat about `make build-css` is removed once it no longer applies
