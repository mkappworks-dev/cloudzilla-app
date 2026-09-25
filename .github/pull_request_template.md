## Summary

Brief description of what this PR does and why.

Closes #<!-- issue number -->

## Changes

-
-

## Type of change

- [ ] Bug fix (`fix/...` or `bug/...`)
- [ ] New feature (`feat/...`)
- [ ] Technical / infrastructure (`tech/...`)
- [ ] Documentation
- [ ] Refactor / internal improvement

## Checklist

- [ ] `make lint` passes with no issues
- [ ] `go test ./...` passes
- [ ] If `.templ` files were changed, `make generate-templ` was re-run and generated `*_templ.go` files are committed alongside their sources
- [ ] If a new template directory was added, `tailwind/tailwind.config.js` `content` glob was updated
- [ ] If a new page was added, it's registered in `internal/router/router.go`
- [ ] If a route must be reachable before setup is complete, it's in the path check in `internal/middleware/setup.go`
- [ ] If schema changed, a new sequential SQL migration was added under `internal/db/migrations/` and `make migrate` was run
- [ ] PR title follows Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `tech:`, `test:`, `refactor:`, `perf:`, `build:`, `ci:`)
- [ ] PR is focused on a single concern
