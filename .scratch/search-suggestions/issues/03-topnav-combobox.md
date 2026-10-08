# Topnav search combobox: debounce, keyboard, `/` shortcut

Created: 2026-10-08
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## What to build

In `internal/view/layout/layout.templ`:

- Form `x-data` with `open`, `active`, `items()`, `move(delta)`, `choose()`, `close()`. This is the rebase point with `feat/search-clear-and-wider-topnav`: fold its `q` in and keep its widths if it has merged.
- Input: `role="combobox"`, `aria-expanded`, `aria-controls`, `aria-activedescendant`, `autocomplete="off"`, `hx-get="/search/suggest"`, `hx-trigger="input changed delay:200ms"`, `hx-target="#topnav-suggest"`, `hx-sync="this:replace"`, and the arrow/enter/escape key handlers (Enter only prevents default when a row is active).
- Dropdown `#topnav-suggest`: `x-show="open"`, `@click.outside="close()"`, an after-swap handler that resets `active` to -1 and sets `open` from whether the fragment has options. Check the htmx 4 event names against other handlers in the repo first.
- Replace the `⌘K` badge with `/` and add a `keydown` listener that focuses the field, ignoring inputs, textareas, selects, contenteditable and modifier keys.

## Acceptance criteria

- [ ] Verified in the browser on a throwaway server and scratch DB: debounce, arrows with wrap, Enter on a row and with none active, Escape, click outside.
- [ ] Rapid typing never leaves a stale list (check the `hx-sync` ordering).
- [ ] `/` focuses the field and is ignored while typing elsewhere; ⌘K still opens the command palette.
- [ ] Signed-in and anonymous views of a private repo checked end to end.
- [ ] `make generate-templ`, `go build ./...`, `go vet ./...`, `make lint`, `make test` and `make test-integration` pass.

## Blocked by

02
