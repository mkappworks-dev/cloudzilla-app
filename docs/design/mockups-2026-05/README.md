# UI mockups — May 2026

Frozen design input for the UI overhaul that shipped as phases 0–9. Archived here because it existed on one laptop and nowhere else; it is reference material, not source.

Start at [`index.html`](./index.html), which links the main pages.

## Read these as Tailwind v3

All 68 pages pull `cdn.tailwindcss.com` — the v3 play CDN — so they need network access to render, and their class names carry v3 semantics. The app builds Tailwind v4, which renamed several scales. **The translation contract is [`../../ui-overhaul-class-map.md`](../../ui-overhaul-class-map.md)**; never copy a class from here straight into a `.templ` file. If a mockup uses something the map doesn't cover, update the map first.

Inline `<style>` blocks in these files did not survive the port and are not a guide to anything in `tailwind/input.css`.

## The shipped UI has moved on

These were drawn before the pages existed, and the product has since diverged on purpose — `actions.html`, for one, was replaced by a Checks tab built from real commit statuses. Where a mockup and the running app disagree, the app is right. Treat a mockup as evidence of original intent, nothing stronger.

## Don't edit these

This directory is a snapshot. New design exploration belongs in a fresh mock under `.scratch/<date>-<slug>/`, next to the spec it serves.
