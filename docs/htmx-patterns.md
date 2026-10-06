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

## Request Parameters

- **POST, PUT and PATCH** send a body: the element's form (`elt.form`, or the closest `<form>`), the submitter's name and value, `hx-include` and `hx-vals`. A button inside a form therefore posts the form's fields too. To send none, empty the body in `hx-on::config:request`, as the "Email me a code" button in `confirm_fields.templ` does (htmx 4 dropped `hx-params`).
- **GET and DELETE** send their parameters in the query string, and include a form only when the requesting element is the form itself. Go's `r.ParseForm` ignores DELETE bodies anyway, so handlers read them with `r.URL.Query()` or `r.FormValue`.

Keep `hx-delete` off forms that hold secrets: a password field, or the `csrf_token` input the layout adds to every form whose `method` isn't GET, would end up in the URL, which reverse proxies record in their access logs. The CSRF token already travels in the `X-CSRF-Token` header.

fetch sends a form body as `application/x-www-form-urlencoded;charset=UTF-8`, so handlers test the media type with `isFormEncoded(r)`, not with string equality.

## Attribute Inheritance

htmx 4 doesn't inherit attributes by default. `hx-target`, `hx-swap`, `hx-confirm`, `hx-headers` and the rest apply only to the element that carries them, unless the ancestor writes them with the `:inherited` suffix. The layout sets `hx-headers:inherited` on `<body>` to send `X-CSRF-Token` with every request. An element with its own `hx-headers` replaces that value and gets a 403 on its first non-GET request.

## Error Responses

htmx 4 swaps 4xx and 5xx responses by default. `htmxConfig` in `layout.templ` turns that off (`noSwap: [204, 304, "4xx", "5xx"]`), because handlers answer errors with JSON (`writeError`) and the layout's `htmx:response:error` listener shows its `error` field as a toast. It also sets `defaultTimeout: 0`: htmx 4 aborts requests after 60 seconds by default, and a merge can take longer.

A form in a modal dialog shows its error inline instead, where a toast would sit behind the backdrop, and so does a validation error on a page form, next to the fields (the repo settings General form). `renderFormError` answers 200 with `HX-Retarget` pointing at the form's error slot, so the form tells success from error by `ctx.hx.retarget`.

A settings form that saves with `hx-post` answers through helpers in `internal/handler` that keep the old response for a non-HTMX request: `settingsError` answers HTMX with JSON, `redirectAfterSave` with `HX-Redirect` and a 204, and `refuseSettingsForm` puts a `SettingsErrorMessage` code in the form's error slot (or redirects to `/settings?profile_error=…`). A one-time secret the next page shows, such as a new token or backup codes, still reaches it: the flash cookie rides on the `HX-Redirect` response.

## hx-on Attributes

htmx 4 event names use colons: `htmx:after:request`, `htmx:after:swap`, `htmx:response:error`. Bind them as `hx-on::after:request`, short for `hx-on:htmx:after:request`. htmx 4 ignores the 2.x spellings `hx-on--after-request` and `hx-on::after-request`, and with one colon, `hx-on:after:request` listens for a DOM event called `after:request`, which htmx never fires.

The handler runs with the event's `detail` in scope, which for htmx events is `{ctx}`:

- Test success with `ctx.response.status<400`; 2.x's `event.detail.successful` is gone.
- Read response headers from `ctx.hx`, lowercased without dashes: `HX-Retarget` is `ctx.hx.retarget`. Once htmx applies it, `ctx.target` holds that selector string, not an element.
- Events bubble, so a form's handler also runs for requests from elements inside it, such as the Markdown preview button or "Email me a code". Guard it with `event.target===this`.

templ compiles an attribute that starts with `hx-on:` and has an `{ expr }` value as a script attribute. Pass a computed handler through `templ.Attributes{…}...` instead, as `repo_transfers.templ` does.

## Toasts

- **From the handler:** call `toast(w, type, message)` before writing the body. It sets `HX-Trigger: {"toast": …}`, and `ToastContainer` shows it.
- **`data-toast` on the requesting element:** shown when the request succeeds. `htmx:after:request` fires before the swap, so this works even when the response swaps the element out. Don't also send a handler toast for that request, or both show. The listener skips a 4xx, a 5xx and any response carrying `HX-Retarget`: `renderFormError`'s form errors come back as 200s, so send that header only with form errors. When the response carries `HX-Redirect` or `HX-Refresh`, the listener stashes the toast for the next page instead.
- **Across a reload or redirect:** stash the toast in `sessionStorage` under `cz-toast`, and the next page shows it. An `hx-on::after:request` handler that stashes one should look for `ctx.hx.redirect` or `ctx.hx.refresh` rather than trust the status alone, because form errors come back as 200 swaps. It should also check `event.target===this`, which skips requests that bubble up from controls inside the element, like the `ConfirmFields` email-code button. `stashToast` in `internal/view/pages/repo_transfers.templ` does both. A plain `<form method="POST" data-toast="…">` is stashed on submit, before the server answers, so a failed save still shows it on the next page: give a form whose handler can fail `hx-post` instead.

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
