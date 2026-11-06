# Ticket 12 verification

The visual harness captures every route in `tests/a11y/routes.js` and every standing HTML mock in light and dark at
1440 × 900, using the generated application and mock Tailwind stylesheets.

Run `npm run screenshots` in `tests/a11y` against the disposable seeded server to regenerate the images. Screenshot
artifacts stay local under `tests/a11y/.cache/screenshots/<theme>/` and are not committed.

Validation: 159 Playwright checks passed, including both themes, keyboard navigation and an injected serious
violation. `go test ./...`, `make lint`, `npm run fmt:check`, and `git diff --check` passed.

The GPT-6.1 Sol medium standards review found no actionable findings. The spec review findings were addressed with
final URL/query checks, shared-select mock parity, and the reproducible screenshot harness.
