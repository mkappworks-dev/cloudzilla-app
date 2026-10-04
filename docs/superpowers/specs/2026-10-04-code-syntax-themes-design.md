# 2026-10-04 — Syntax highlighting with per-user code themes

**Status:** Design approved.
**Branch:** `feat/code-syntax-themes`
**Affected subsystems:** new `internal/highlight` package, `CodeService` (blob, blame, diff highlighting), `internal/markdown`, `UserService`/`UserStore` (two new `users` columns), settings page + handler, layout, static assets, docs.

---

## Background

Code renders as plain monochrome text everywhere. `PageBlob`'s doc comment says "syntax-highlighted numbered lines", but `blob.templ` prints `{ line.Text }`. Fenced Markdown blocks render as `<pre><code class="language-go">` with no colors. A user-selectable code theme therefore needs syntax highlighting first.

The site's light/dark toggle is client-side only: `<html>` carries `.dark` (the default) or `.light`, set from `localStorage['cz-theme']` before paint and flipped by `#theme-toggle` with no reload.

## Locked decisions

1. **Surfaces:** file view (blob page and the inline file on the tree page), blame, gists, diffs (commit page and PR "Files changed": unified, split and whole-file tables), and fenced Markdown code blocks.
2. **One theme per mode.** Settings offers "Light mode code theme" and "Dark mode code theme". The active one follows the site toggle instantly. Defaults: `github` / `github-dark`.
3. **Curated catalog.** Each dropdown shows only its own kind of theme.
   - Light: GitHub (`github`), Solarized Light (`solarized-light`), Catppuccin Latte (`catppuccin-latte`), Gruvbox Light (`gruvbox-light`), Tokyo Night Day (`tokyonight-day`), Rosé Pine Dawn (`rose-pine-dawn`).
   - Dark: GitHub Dark (`github-dark`), One Dark (`onedark`), Dracula (`dracula`), Monokai (`monokai`), Nord (`nord`), Solarized Dark (`solarized-dark`), Catppuccin Mocha (`catppuccin-mocha`), Tokyo Night (`tokyonight-night`), Gruvbox (`gruvbox`).
   - IDs are chroma style names.
4. **Approach A: server-side Chroma, class-based output.** HTML carries token classes only. A build-time stylesheet holds every catalog theme, scoped by attributes on `<html>`.

Out of scope: code search snippets, the web file editor, an instance-wide default theme, and highlighting cache.

---

## 1. `internal/highlight` package

A pure package with no DB or HTTP dependencies, the same role `internal/markdown` plays. It depends on `github.com/alecthomas/chroma/v2` (v2.27.0).

### API

```go
// Lines returns one HTML fragment per "\n"-separated line of src, or nil when
// src should render as plain text (no lexer, over a cap, or lexing too slow).
func Lines(filename, src string) []template.HTML

// Block returns src highlighted as one fragment for a <pre>, or "" for plain text.
// lang is a Markdown info string ("go", "py"); filename is used when lang is "".
func Block(lang, filename, src string) template.HTML
```

### Lexer selection

- By filename: `lexers.Match(path.Base(filename))`.
- Then, when there's no match and the file has no extension, `lexers.Analyse` on the first 1 KiB. This catches shebang scripts.
- For Markdown: `lexers.Get(lang)`. Unknown `lang` means plain.
- The plaintext lexer and "no lexer" both return nil/"". Callers keep their current plain rendering.
- The lexer is wrapped in `chroma.Coalesce`.

### Output

- Each token becomes `<span class="hl-<short>">` + `html.EscapeString(value)` + `</span>`. `<short>` is the token type's `chroma.StandardTypes` short name (`k`, `s2`, `c1`, …).
- `Text`, `Whitespace` and `Error` tokens are emitted escaped, with no span. Some themes give `Error` a red background, and partial input (a diff fragment, an unfinished fence) trips it constantly.
- Class names come only from the fixed `StandardTypes` table, and every value is escaped, so the output is safe for `template.HTML`.
- `Lines` splits with `chroma.SplitTokensIntoLines` and strips the trailing `"\n"` from each line. It returns exactly `len(strings.Split(src, "\n"))` entries. If the count differs (some lexers add a trailing newline via `EnsureNL`), it trims the extra empty line or returns nil.

### Guards

- **Size cap:** skip (return plain) when `len(src) > 512 KiB`.
- **Deadline:** chroma runs `regexp2` with a 250 ms per-match timeout, which doesn't bound total time. Tokenising iterates tokens and aborts, returning plain, once **500 ms** have elapsed for one call. Total time per call is therefore bounded at roughly the deadline plus one match timeout.
- A `Budget` type gives diffs a shared cap per page: `NewBudget(maxBytes int, maxTime time.Duration)`, with `LinesWithin(b *Budget, filename, src string)`. Files past the budget render plain.

### Theme catalog (`themes.go`)

```go
type Theme struct{ ID, Name string; Dark bool }
var Themes = []Theme{...}            // the catalog above, in display order
const DefaultLight, DefaultDark = "github", "github-dark"
func NormalizeLight(id string) string // id if it's a light catalog theme, else DefaultLight
func NormalizeDark(id string) string
```

### Stylesheet

`Stylesheet() []byte` builds the CSS for every catalog theme from `chroma/v2/styles.Registry[id]`. For each theme:

```css
html.dark[data-code-dark="dracula"] .hl { background-color: #282a36; color: #f8f8f2 }
html.dark[data-code-dark="dracula"] .hl-k { color: #ff79c6 }
html:not(.dark)[data-code-light="github"] .hl-k { color: #cf222e }
```

- Declarations come from `html.StyleEntryToCSS(style.Get(tt))`, for every `StandardTypes` token type except `Text`, `Whitespace`, `Error` and `Background`.
- A type whose CSS is empty, or equal to the base `Text` entry, is skipped.
- `.hl` gets the theme's `Background` entry: background color plus base text color.
- Output is deterministic: themes in catalog order, token types sorted.

`cmd/gen-code-themes/main.go` writes `Stylesheet()` to `cmd/server/frontend/static/code-themes.css`, which is committed. A `generate-code-themes` Makefile target runs it. A test fails when the committed file differs from `Stylesheet()`, with a message naming the target.

---

## 2. Delivering the theme to the page

- `layout.Base` adds `<link rel="stylesheet" href={ assets.URL("/static/code-themes.css") }/>` after `main.css`.
- It also renders `data-code-light={ base.CodeThemeLight() }` and `data-code-dark={ base.CodeThemeDark() }` on `<html>`.
- `view.BasePage` gains `CodeLight, CodeDark string` fields. The accessor methods return `highlight.DefaultLight/Dark` when they're empty, so pages that build `BasePage` without `basePage()` (setup, errors) still get a theme.
- `basePage()` fills them for a signed-in user from `Services.User.CodeThemes(ctx, userID)`, one primary-key query. On error it logs and uses the defaults, matching how the unread-count failure is handled.
- Anonymous visitors get the defaults.
- The site toggle needs no change: CSS picks the theme from `.dark` plus the attribute.

Code containers get the `hl` class: the blob, tree-file and blame scroll containers, each diff table wrapper, the gist `<pre>`, and highlighted Markdown `<pre>`. That class supplies the theme background and base color. Row tints (`bg-success/10`, `bg-destructive/10`, `target:bg-accent/30`, `hover:bg-muted/50`) are translucent and still layer on top.

---

## 3. Persistence and settings

### Migration and data layer

- **Migration `101_user_code_themes.sql`** (renumber if `main` has moved):
  ```sql
  ALTER TABLE users
    ADD COLUMN code_theme_light TEXT NOT NULL DEFAULT 'github',
    ADD COLUMN code_theme_dark  TEXT NOT NULL DEFAULT 'github-dark';
  ```
  There's no CHECK constraint, so the catalog can change without a migration. Unknown stored values are normalized when read.
- **Model:** `User.CodeThemeLight`, `User.CodeThemeDark` (`json:"-"`), added to `userColumns` and `scanUser`.
- **Store:**
  - `UpdateCodeThemes(ctx, userID, light, dark string) error`
  - `GetCodeThemes(ctx, userID) (light, dark string, err error)`, which selects only the two columns.
- **Service:**
  - `UserService.UpdateCodeThemes(ctx, userID, light, dark)` normalizes both through `highlight.Normalize*`, so an invalid value saves the default, as `UpdateNotificationPrefs` does for digest mode.
  - `UserService.CodeThemes(ctx, userID)` returns the normalized pair.

### Handler and route

- `POST /settings/appearance` (`authMW`) → `UpdateAppearanceSettings`.
- It parses `code_theme_light` and `code_theme_dark`. On an HTMX request it returns 204, otherwise it redirects to `/settings#appearance`. This mirrors `UpdateNotificationSettings`.
- `GET /settings/appearance` → `handler.MovedPermanently("/settings", "appearance")`.

### Settings page

There's a new **Appearance** section between Profile and Security, plus a nav link.

- One form, `hx-post="/settings/appearance" hx-trigger="change" hx-swap="none" data-toast="Code theme saved"`.
- Two `components.Select` rows: "Light mode code theme" and "Dark mode code theme".
- A preview: a short Go snippet rendered through `highlight.Block` inside `<pre class="hl">`. It's built in the handler and passed as `SettingsData.CodePreview template.HTML`.
- A small inline script listens for `change` on each select and sets `document.documentElement.dataset.codeLight` / `codeDark`. The preview, and every code block on the page, update before the save round-trip.
- Helper text: "The preview follows the site theme. Toggle it to preview the other mode."

---

## 4. Applying highlighting per surface

| Surface | Where | Change |
| --- | --- | --- |
| Blob and tree file | `CodeService.GetBlob` | `CodeLine` gains `HTML template.HTML` (`json:"-"`), filled from `highlight.Lines(path, contents)`. Templates render `templ.Raw(line.HTML)` when non-empty, else `line.Text`. |
| Blame | `CodeService.GetBlame` | The same for `BlameLine.HTML`. `components.BlameLine` gains `CodeHTML`. |
| Gist | gist page handler | It builds per-file `highlight.Block("", f.Filename, f.Content)` into the view-model. The `<pre>` gets `hl` and renders it raw when non-empty. |
| Markdown | `internal/markdown` | A decorator on `ast.KindCodeBlock`, chained with the mermaid one. When `highlight.Block(lang, "", value)` returns non-empty, it emits `<pre class="hl"><code class="language-<escaped lang>">…</code></pre>`. Otherwise it defers to the next renderer. Mermaid, unknown-language and indented blocks render exactly as today. |
| Diffs | `CodeService.HighlightDiffs(owner, repoName string, files []FileDiff)` | See below. |

### Diffs

`GetCommit` runs for every pushed commit in post-receive contributor stats. `GetPullDiff` runs in CODEOWNERS and reviewer suggestions. Neither may pay for highlighting, so diffs are highlighted by an explicit call that only the commit page and the PR files page make.

- `FileDiff` gains unexported `oldBlob, newBlob plumbing.Hash`. `GetCommit` and `GetPullDiff` set them from `fp.Files()` hashes, which costs nothing.
- `DiffLine` gains `HTML template.HTML` (`json:"-"`).
- `HighlightDiffs` opens the repo once and creates one `highlight.Budget` (4 MiB, 2 s). For each non-binary file it reads the old blob (unless `IsNew`) and the new blob (unless `IsDelete`), then runs `LinesWithin` for each, using `NewPath`, or `OldPath` for deletes.
- Each `DiffLine` is mapped by line number:
  - `del` maps to `old[OldNum-1]`.
  - `add` and `ctx` map to `new[NewNum-1]`.
- **Invariant:** HTML is set only when the index is in range and that line's plain source text equals `DiffLine.Content`. A line that fails the check keeps empty HTML and renders plain, so a mismatch (CRLF, a `\ No newline` marker, a hash read failure) never shows wrong code.
- A blob read error is logged at warn level and that file renders plain.

Templates (`components.DiffHunkTable`, `pr_files.templ` unified/whole/split):

- The `+`/`-`/space marker keeps its success/destructive/muted color.
- The code cell drops `text-success`/`text-destructive`/`text-foreground/85` when `line.HTML` is non-empty, so token colors show. Row backgrounds still mark adds and deletes.
- The split view's `prSplitCellData` gains `HTML template.HTML`, copied alongside `Content`.

---

## 5. Testing

- **`internal/highlight`:**
  - A Go file yields keyword and string spans.
  - `<script>` inside a string literal comes out escaped.
  - The line count equals `strings.Split` for files with and without a trailing newline.
  - An unknown extension, plaintext, and anything over 512 KiB return nil.
  - The budget stops highlighting after its byte cap.
  - The deadline aborts. Test it through an injectable clock or a tiny deadline on a large input.
  - Every catalog ID exists in `styles.Registry`; `styles.Get` silently falls back, so the test must check the registry. Each theme's `Dark` flag matches the luminance of its background.
  - `Stylesheet()` equals the committed `code-themes.css`.
  - `NormalizeLight/Dark` reject cross-mode and unknown IDs.
- **`internal/markdown`:**
  - A `go` fence is highlighted and keeps `class="language-go"`.
  - `mermaid`, unknown-language, malicious-language (`"><script>`) and indented blocks are unchanged. Update the existing expectations that assumed plain output for known languages.
- **`CodeService` (temp repo):**
  - A blob gets `HTML` per line.
  - `HighlightDiffs` on a change inside a multi-line block comment that begins before the hunk colors the hunk's lines as comment. This is the reason for full-file highlighting.
  - A forced content mismatch leaves `HTML` empty.
  - Binary, new and deleted files work.
- **Store/service (integration, `TEST_DATABASE_DSN`):**
  - Defaults after the migration.
  - `UpdateCodeThemes` round-trip.
  - An invalid or cross-mode ID saves the default.
- **Router page check:**
  - `/settings` renders the Appearance section.
  - `<html>` carries the user's `data-code-light`/`data-code-dark`, and the defaults when signed out.
  - `POST /settings/appearance` returns 204 on HTMX and persists.
- **Browser, on a throwaway server:**
  - Blob, blame, commit diff, PR unified and split diffs, gist and README are colored.
  - The theme toggle switches code themes without a reload.
  - A settings change updates the preview live and survives a reload.

## 6. Docs

- `docs/code-browser.md`: a "Syntax highlighting" section covering the `internal/highlight` API, caps and deadline, the diff mapping invariant, and why `HighlightDiffs` is opt-in.
- Regenerating `code-themes.css` with `make generate-code-themes`.

## Risks

- `.worktrees/feat+blob-page-file-tree` has uncommitted work around the blob page. Keep the `blob.templ` and `tree.templ` edits to the code cell and container class to make merges small.
- Migration numbers collide across parallel branches; recheck `origin/main` before committing.
- `code-themes.css` adds roughly 15 themes × ~80 rules (low tens of KB gzipped), cached for a year via `assets.URL`.
