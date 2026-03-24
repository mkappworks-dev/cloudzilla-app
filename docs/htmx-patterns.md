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
