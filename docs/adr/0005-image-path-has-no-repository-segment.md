# 5. An image path has no repository segment; a package links to one repository

Status: accepted, 2026-11-18

## Context

The registry needs a path for each image. GitLab puts the project in it (`host/group/project/image`), which ties an
image to its code but makes every image path long and breaks on a project rename. ghcr.io and Forgejo use
`host/owner/image` and link the image to a repository separately. Image paths end up in deployment manifests, CI files
and other people's `docker pull` commands, so the choice is expensive to change once images are published.

## Decision

An image lives at `{host}/{owner}/{name}`. Its first manifest push links it to one repository of that owner: a
repository named like the image's first segment always wins (and a pusher who can't read it is refused like a name that
matches nothing), otherwise the `org.opencontainers.image.source` label names one the pusher manages. The package takes
all its permissions from that repository. The path owner keeps the right to delete it or re-link it to another of its
own repositories, also after the repository is transferred to someone else; re-linking never crosses owners.

## Considered options

A repository segment in the path (GitLab's form) needs no linking rule but breaks pulls on every repository rename and
transfer. Letting the label decide first (ghcr's form) lets one team claim another team's image name inside an
organization. Per-package visibility (ghcr) was rejected as a second permission model to keep in sync. Accepted costs:
an owner rename breaks pulls under the old name, and a writer in an organization can tell a free image name from a taken
one.
