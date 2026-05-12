## Summary

Brief description of what this PR does and why.

Closes #<!-- issue number -->

## Changes

-
-

## Type of change

- [ ] Bug fix (`bug/...`)
- [ ] New feature (`feat/...`)
- [ ] Technical / infrastructure (`tech/...`)
- [ ] Documentation (`docs/...`)
- [ ] Refactor / internal improvement

## Checklist

- [ ] `make lint` passes with no issues
- [ ] `go test ./...` passes
- [ ] If `.templ` files were changed, `templ generate` was re-run and generated `*_templ.go` files are committed alongside their sources
- [ ] If a new template directory was added, `tailwind/tailwind.config.js` `content` glob was updated
- [ ] If a new page was added, it's registered in `internal/router/router.go` and listed in `pageNames`
- [ ] If a route must be reachable before setup is complete, it's in the allowlist in `middleware/setup.go`
- [ ] If schema changed, a new sequential SQL migration was added under `migrations/` and `make migrate` was run
- [ ] PR title follows Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `tech:`, `test:`, `refactor:`, `perf:`, `build:`, `ci:`)
- [ ] PR is focused on a single concern
