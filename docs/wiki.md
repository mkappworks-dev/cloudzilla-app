# Wiki

Each repository has a wiki of Markdown pages, stored as files in a bare git repository at `<git.repos_root>/<owner>/<repo>.wiki.git`. It is created by the first page save, on a `main` branch, and moves with the repository when it is deleted, restored, purged or transferred. Pages are not cloneable over HTTP or SSH: the web editor and `CodeService` are the only writers.

## Pages

- A page is `<slug>.md` at the root of the wiki tree. A slug matches `^[A-Za-z0-9_-]{1,100}$` (`validWikiSlug`); anything else is 400.
- `/{owner}/{repo}/wiki` redirects to the `Home` page.
- The sidebar lists pages in the order of the `.order` file (one slug per line), then any page it does not name alphabetically. A page's title is its first heading.
- Every save, rename, delete and reorder is one commit authored by the signed-in user (`UserService.CommitAuthor`).
- Renaming a page (`new_slug` on save) rewrites `.order` in the same commit, so its sidebar position survives. A `new_slug` that already exists is 409.

## Access

| Action                                  | Needs     | Refusal                                            |
| --------------------------------------- | --------- | -------------------------------------------------- |
| View a page                             | CanRead   | 404, so a private wiki is not revealed             |
| Create, edit, rename, reorder           | CanWrite  | 403                                                |
| Delete a page                           | CanManage | 403                                                |

The wiki can be turned off per repository with the **Wiki** feature toggle in repository settings (`repositories.allow_wiki`, migration 058). A disabled wiki answers 404 on every route above. It also answers 404 when a repository named `<repo>.wiki`, created before that suffix was reserved, holds the directory.

## Storage and conflicts

The wiki's disk use counts toward the owner's storage [quota](./configuration.md#quotas); a save at or over it returns `403` (the same message as a web commit), and a successful save, delete or reorder re-measures the repository. A change commits against the branch tip it read; if a push moved the branch meanwhile, the request returns `409` and the client reloads and retries (`service.ErrRefMoved`, as for [pr-merge](./pr-merge.md#merge-flow)).

## Routes

| Method | Path                                      | Auth                 | Description                                                  |
| ------ | ----------------------------------------- | -------------------- | ------------------------------------------------------------ |
| GET    | `/{owner}/{repo}/wiki`                    | Optional             | Redirect to `Home`                                           |
| GET    | `/{owner}/{repo}/wiki/new`                | Required, CanWrite   | New page form                                                |
| GET    | `/{owner}/{repo}/wiki/{slug}`             | Optional             | A page                                                       |
| GET    | `/{owner}/{repo}/wiki/{slug}/edit`        | Required, CanWrite   | Edit form                                                    |
| POST   | `/api/repos/{owner}/{repo}/wiki/{slug}`   | Required, CanWrite   | Create or update: `content`, `message`, `new_slug`; 303 to the page |
| POST   | `/api/repos/{owner}/{repo}/wiki/order`    | Required, CanWrite   | `order`: comma-separated slugs; 200                          |
| DELETE | `/api/repos/{owner}/{repo}/wiki/{slug}`   | Required, CanManage  | Delete a page; HTMX gets 200, otherwise 303 to the wiki      |

The `order` route sits before `{slug}`, so a page cannot be named `order` through the API.
