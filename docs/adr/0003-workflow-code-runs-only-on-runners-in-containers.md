# 3. Workflow code runs only on separate runners, in containers

Status: accepted, 2026-11-13

## Context

A Workflow is code written by whoever can push to a Repository, and on a public Repository a stranger's pull request
proposes more. The server process holds the database DSN, `security.secret_key` and every sealed credential, and it
serves the git data of all Repositories. Running a step inside it, or as its user on its host, makes any writer an
Instance superadmin.

## Decision

Workflow code runs only in a container started by a `cz-runner` process, which registers with a scoped token and polls
the server over outbound HTTPS. The server has no Docker access and never connects to a Runner. Containers are
non-privileged, with no host mounts and dropped capabilities. There is no host executor in v1. Fork pull requests wait
for a writer's approval and run with no secrets and a read-only per-job token.

## Considered options

A runner embedded in the server was the simplest to deploy but needs the Docker socket on the server, so a container
escape lands next to the database. A host executor behind a label, as Gitea has, is faster for a small team but is the
setup every vendor's security guidance warns against. Both were rejected; a host executor can be added later behind an
explicit instance setting without changing this decision for the default.
