# Mockup → shadcn class translation map

Source of truth for the UI overhaul (spec: [2026-05-14-ui-overhaul-design.md](superpowers/specs/2026-05-14-ui-overhaul-design.md)). Every per-phase page port translates mockup classes mechanically using this map. If a mockup uses a class not in this map, **halt and update the map first** — do not invent ad-hoc replacements during a port.

## Token reference (CSS variables)

| Mockup variable | shadcn variable in `tailwind/input.css` | Tailwind utility |
|---|---|---|
| `--bg` | `--background` | `bg-background` |
| `--surface` | `--card` | `bg-card` |
| `--elevated` | `--accent` | `bg-accent` |
| `--border` | `--border` | `border-border` |
| `--border-strong` | `--border-strong` (new — added in Task 3) | `border-border-strong` |
| `--fg` | `--foreground` | `text-foreground` |
| `--fg-2` | `--muted-foreground` | `text-muted-foreground` |
| `--fg-3` | `--muted-foreground` at 70% opacity | `text-muted-foreground/70` |
| `--focus` | `--ring` | `focus-visible:outline-ring` |
| `--link` | `--primary` | `text-primary` |
| `--success` | `--success` | `text-success`, `bg-success` |
| `--warn` | `--warning` | `text-warning` |
| `--danger` | `--destructive` | `text-destructive` |

## Utility classes

| Mockup class | shadcn replacement | Notes |
|---|---|---|
| `mono` | `font-mono` | |
| `fg2` | `text-muted-foreground` | |
| `fg3` | `text-muted-foreground/70` | |
| `b` (border container) | `border-border` | combine with `border` Tailwind utility |
| `bs` (stronger border) | `border-border-strong` | requires the `--border-strong` token (Task 3) |
| `surface` | `bg-card` | |
| `elevated` | `bg-accent` | |
| `hover-row` | `hover:bg-accent` | apply on table rows / list items |
| `link` | `text-primary` | |
| `tab-active` | `text-foreground border-foreground` | apply to active tab anchor |
| `prose` | `prose` | Long-form Markdown (README, PR description). Use the `@tailwindcss/typography` plugin if installed; otherwise add a minimal `.prose` ruleset to `tailwind/input.css` under `@layer components`. |

## Component-replaced classes

These mockup classes correspond to components in `internal/view/components/`. Use the component, do not copy the class.

| Mockup classes | Replace with component |
|---|---|
| `dropdown-wrap`, `dropdown-panel`, `dropdown-item`, `dropdown-divider`, `dropdown-header` | `components.DropdownMenu*` |
| `avatar` | `components.Avatar` |
| `badge`, `badge--open`, `badge--closed`, `badge--merged`, `badge--draft` | `components.Badge` (variant prop carries the state) |
| `skip-link` (link element only) | already in layout |

## Language chip classes (`lbl-*`)

The mockup uses `.lbl` (base chip) and `.lbl-blue`, `.lbl-green`, `.lbl-amber`, `.lbl-purple` colour variants for language chips on gist rows. These are small inline pills (10px, 1px-7px padding, pill shape). Render them with inline Tailwind — no separate component, just a `<span>` with the classes below.

| Mockup class | Tailwind replacement |
|---|---|
| `lbl` (base, no colour) | `inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border border-border bg-accent text-muted-foreground whitespace-nowrap` |
| `lbl-blue` | `inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-primary/15 text-primary border-primary/30 whitespace-nowrap` |
| `lbl-green` | `inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-success/15 text-success border-success/30 whitespace-nowrap` |
| `lbl-amber` | `inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-warning/15 text-warning border-warning/30 whitespace-nowrap` |
| `lbl-purple` | `inline-flex items-center text-[10px] leading-none px-1.5 py-0.5 rounded-full border bg-[hsl(270_70%_50%/0.15)] text-[hsl(270_70%_70%)] border-[hsl(270_70%_50%/0.30)] whitespace-nowrap` |

In templ files, pass the correct Tailwind string through a helper (e.g. `gistLanguage(filenames)` returns `(label, chipClass string)`). Use `chipClass` as the class attribute on the `<span>`.

## Promoted utility classes (no shadcn equivalent)

These classes get added to `tailwind/input.css` as project-level utilities in Task 3. They are used **by their original names** in templ files.

- `grid-bg` — radial-masked grid background on home hero / auth pages
- `heatmap-cell-0`, `heatmap-cell-1`, `heatmap-cell-2`, `heatmap-cell-3`, `heatmap-cell-4` — commit heatmap intensity scale
- `dark-only`, `light-only` — already in layout (kept)

## Inline `<style>` blocks

**Inline `<style>` blocks in mockup HTML files do not survive the port.** Anything inside an inline `<style>` either:
1. Translates to Tailwind utilities (via this map), or
2. Gets promoted to `tailwind/input.css` (only `grid-bg` and the `heatmap-cell-*` scale qualify so far).

## Updating this map

When a per-phase port discovers a mockup class missing from this map:
1. Stop the port.
2. Decide which case it is:
   - Has a clean shadcn equivalent → add row to the utility classes table.
   - No shadcn equivalent but reusable → add to the promoted utilities section AND to `tailwind/input.css`.
   - One-off → translate inline using a comment in the templ file (`<!-- mockup: .xyz -->`).
3. Commit the map update separately from the port commit, with subject `docs(ui): extend class translation map for <reason>`.
