# HTMX Patterns & Template Parsing

## HTMX Pattern (Example: Close Issue)

Template:

```html
<div id="issue-detail" ...>
  {{if eq .Issue.State "open"}}
  <button
    hx-patch="/api/repos/{{.Owner}}/{{.Repo}}/issues/{{.Issue.Number}}"
    hx-vals='{"state":"closed"}'
    hx-target="#issue-detail"
    hx-swap="outerHTML"
  >
    Close Issue
  </button>
  {{end}}
</div>
```

Handler:

```go
func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
    // ... fetch and update issue ...
    if r.Header.Get("HX-Request") == "true" {
        h.renderFragment(w, "fragment-issue-detail", IssueDetailFragData{...})
        return
    }
    writeJSON(w, http.StatusOK, issue)
}
```

Fragment (`templates/fragments/issue_detail.html`):

```html
{{define "fragment-issue-detail"}}
<div id="issue-detail" ...>
  <!-- Rendered state after toggle -->
  {{if eq .Issue.State "open"}}...{{end}}
</div>
{{end}}
```

HTMX flow: button click → PATCH → handler returns fragment → HTMX replaces `#issue-detail` outerHTML

## DELETE Parameters

htmx 2 sends a DELETE's parameters (`hx-vals`, `hx-include`, the enclosing form's fields) in the query string, not the body; Go's `r.ParseForm` ignores DELETE bodies anyway. Handlers read them with `r.URL.Query()` or `r.FormValue`.

Keep an `hx-delete` out of forms that hold secrets: a password field, or the `csrf_token` input the layout adds to every form whose `method` isn't GET, would end up in the URL, which reverse proxies record in their access logs. The CSRF token already travels in the `X-CSRF-Token` header.

## hx-on Attributes

Bind htmx events as `hx-on::after-request` or `hx-on--after-request`, both short for `htmx:after-request`, or name the event in full: `hx-on:htmx:after-request`. With one colon and a bare name, `hx-on:after-request` listens for a DOM event called `after-request`, which htmx never fires.

## Toasts

- **From the handler:** call `toast(w, type, message)` before writing the body. It sets `HX-Trigger: {"toast": …}`, and `ToastContainer` shows it.
- **`data-toast` on the requesting element:** shown after a successful request, even when the response swaps the element out. Don't also send a handler toast for that request, or both show.
- **Across a reload or redirect:** stash the toast in `sessionStorage` under `cz-toast`, and the next page shows it. `HX-Redirect` and `HX-Refresh` responses leave `event.detail.successful` unset, so gate the stash on `event.detail.xhr.status<300`, as `stashToast` in `internal/view/pages/repo_transfers.templ` does. A plain `<form method="POST" data-toast="…">` is stashed on submit.

## Template Parsing

Templates are parsed at startup in `router.mustParseTemplates()`:

1. Create a `template.FuncMap` with custom helpers (`add`, `percent`)
2. Parse `layout.html` into a base template with the FuncMap
3. Clone base template for each page, parse page file into clone
4. Parse all fragments into a shared template set (also with the FuncMap)
5. Store page clones in map, fragment set in handler
6. Handler calls `tmpl.ExecuteTemplate(w, "layout", data)` for pages or `frags.ExecuteTemplate(w, "fragment-NAME", data)` for fragments

This pattern avoids Go template's global `define` namespace issue.

**Custom FuncMap helpers** (defined in `router.go`):

- `add a b` — integer addition (`{{add .OpenCount .ClosedCount}}`)
- `percent part total` — integer percentage, 0 when total=0 (`{{percent .ClosedCount $total}}`)

Page templates cannot call fragment templates directly (they are in separate template sets). Fragment templates are only used as HTMX swap responses from handlers. Inline the shared HTML in page templates if needed.

## Go Template Attribute Rules

**Keep all `{{if}}` inside `class`/`style` attributes on a single line.** The VS Code HTML formatter wraps long attribute values across multiple lines and inserts a leading space into the first string literal after the break — turning `"open"` into `" open"`, which will never match `.State`. Project-scoped `.vscode/settings.json` disables HTML format-on-save, but keeping conditionals on one line is the belt-and-suspenders fix.

Correct:

```html
<span class="... {{if eq .State "open"}}bg-green-100{{else}}bg-red-100{{end}}">
```

Broken (after formatter):

```html
<span class="...
  {{if eq .State " open"}}bg-green-100{{else}}bg-red-100{{end}}">
```

## Scripts That Act on Swapped Content

A script that enhances server-rendered markup must also run on `htmx:afterSwap`, or content swapped in later (an editor's Preview tab, a newly posted comment) stays raw. The mermaid loader in `layout.templ` is the model. On `DOMContentLoaded` and on every `htmx:afterSwap` it looks for `.mermaid:not([data-processed])`. Only when it finds one does it load `mermaid.min.js` (once per page) and call `mermaid.run()`, so pages without diagrams never download it. A fragment that renders markdown needs no script of its own.

Load such scripts through `assets.URL(...)` (see `internal/assets`) so their URLs carry a content hash.
