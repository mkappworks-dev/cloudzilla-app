# Highlight the matched text

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: 04-results-page-rows

## What

Add `components.Highlight(text, query string)`, a templ-renderable helper that wraps each word of `text` starting with a query token in `<mark>`. Tokens split on non-letter/digit and are lowercased, as in `prefixTSQuery`. Use it for repo `owner/name`, org and user names, and issue and PR titles. The row components take a `Title` that is either plain text or a highlighted fragment; keep the default plain so the main pages are unchanged.

## Acceptance criteria

- [ ] `bra` highlights `brave` in `acme/brave-core`; `BRAVE` in a title matches case-insensitively.
- [ ] A match in the middle of a word is not highlighted (`rave` does not mark `brave`).
- [ ] HTML in a title (`<script>`) is escaped, with and without a match.
- [ ] Unicode letters and a query of only punctuation render the text unmarked.
- [ ] Unit tests for the above.

## Relevant files

`internal/view/components/` (new `highlight.templ`), the row components from ticket 03
