# Package registry mock (ROADMAP 16.1–16.2)

Gives container images and generic files one **Packages** surface per repository, plus a Packages tab on the owner's
profile. A package follows its repository: readers pull, writers push and delete, and there is no per-package visibility
switch. Classes come from `table.templ`, `tabs.templ`, `badge.templ`, `dialog.templ`, the clone popover and copy button
on the repo page, and the release detail layout (asset rows, metadata aside).

Open with `python3 -m http.server` from the repo root at `/.scratch/2026-11-06-package-registry/mock.html`. Rebuild CSS
with `bin/tailwindcss -i .scratch/2026-11-06-package-registry/mock-input.css -o .scratch/2026-11-06-package-registry/mock.css`.
`shell.js` has no Packages tab, so each page swaps in a copy of its repo subnav with one added after Releases (generated
from shell.js's own tab markup). Screenshots go in `shots/` (gitignored, not committed).

## Pages and states

- `mock.html`: repo packages table (type icon, name and description, type badge, latest version, downloads, updated),
  type tabs, name filter, and a "Publish" popover with docker and curl snippets. States: `list`, `publish`, `empty`. The
  empty state shows both how-tos and the token scopes.
- `package.html`: install command with copy button (`docker pull <host>/<owner>/<name>:<tag>`, or `curl -LO` for a
  generic file), a tags table (tags, digest, size, publisher, delete) or files plus versions for generic, a readme card,
  and an aside with repository, visibility, platforms, storage and "Delete package". Deleting a version or the package
  asks you to type its name. States: `container`, `private`, `generic`, `delete`.
- `owner.html`: `/{owner}` Packages tab across the owner's repositories, with type and repository filters. It works the
  same for an organization.

## ROADMAP vs mock

| ROADMAP proposes                                                     | Mock assumes                                                                                                                                         |
| -------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `container_images` row per (repo, name, tag)                         | Manifests and tags apart: one digest can carry several tags (`v0.4.1` + `latest`), and untagged manifests are listed and deletable                  |
| `packages` row per (repo, type, name, version) with one `filename`   | One version holds several files (`package_files`: filename, size, sha256), so a version row shows a file count                                     |
| No download counts or descriptions                                   | `download_count` per version/tag; description from the `org.opencontainers.image.description` annotation (blank for generic)                        |
| Image path not stated beyond `/v2/...`                               | `docker pull <host>/<owner>/<name>:<tag>`, linked to a repo by the `org.opencontainers.image.source` label                                          |
| Bearer JWT/PAT                                                       | New PAT scopes `packages:read` (private pulls) and `packages:write` (push and delete)                                                               |
| `GET /{owner}/{repo}/packages`, `/packages/container`                | `/{owner}/{repo}/packages`, `/packages/{type}/{name}`, `DELETE` per version and per package, and `/{owner}?tab=packages`                           |
| Blobs under `<ReposRoot>/../registry/`                               | Shown as "Storage" counting toward the owner's quota; the storage `Backend` (local/S3) is a better home than a sibling directory                  |

## Open questions

- `<host>/<owner>/<name>` has no repo segment, so what links an image to a repository: the source label, a name match,
  or a `<owner>/<repo>/<name>` path?
- New scopes, or reuse `repo:read`/`repo:write`?
- Where does the readme come from: `README.md` at the tag's commit (needs a tag-to-commit link), or a file in the
  package?
- Does deleting a tag also delete its manifest while other tags point at it? The mock's dialog says layers shared with
  other tags stay.
- Should storage count against the owner's storage quota (shown in account settings), and is a garbage-collection pass for orphaned blobs in scope?
- Downloads are counted per version. Are pulls by digest counted too?
