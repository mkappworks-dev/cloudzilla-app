# Contributing to Cloudzilla

Thank you for your interest in contributing to Cloudzilla. This guide covers the process and requirements for contributing to the project.

## Contributor License Agreement (CLA)

All contributors must sign the Contributor License Agreement before their first pull request can be merged. The CLA is managed via the [CLA Assistant](https://github.com/cla-assistant/cla-assistant) GitHub App and will automatically prompt you when you open your first PR.

The full CLA text is available at [.github/CLA.md](.github/CLA.md).

**Key terms:**

- Contributors retain copyright ownership of their contributions.
- The CLA grants Cloudzilla full commercial rights to use, modify, sublicense, and distribute contributions.
- The CLA is a one-time requirement per contributor.

## License

Cloudzilla is licensed under the [Business Source License 1.1 (BSL 1.1)](LICENSE). All contributions are accepted under the same license terms. By submitting a contribution, you agree that your work will be licensed under BSL 1.1.

## Code of Conduct

We are committed to providing a welcoming and productive environment for everyone. All participants are expected to:

- Be respectful and professional in all interactions.
- Provide constructive feedback and accept it graciously.
- Focus on what is best for the project and the community.
- Refrain from any form of harassment, discrimination, or personal attacks.

Violations may result in removal from the project at the maintainers' discretion.

## How to Contribute

### Reporting Bugs

1. Search [existing issues](../../issues) to avoid duplicates.
2. Open a new issue with a clear, descriptive title.
3. Include steps to reproduce, expected behavior, actual behavior, and your environment (OS, Go version, PostgreSQL version).
4. Attach logs or screenshots if applicable.

### Suggesting Features

1. Search existing issues and discussions to check if the idea has already been proposed.
2. Open a new issue with the `enhancement` label.
3. Describe the use case, the proposed solution, and any alternatives you considered.

## Code Submission

We follow a standard fork-and-branch workflow:

1. **Fork** the repository to your GitHub account.
2. **Clone** your fork locally.
3. **Create a branch** from `main` with a descriptive name (e.g., `feat/add-webhook-retry`, `fix/login-redirect`).
4. **Make your changes** in focused, incremental commits.
5. **Push** your branch to your fork.
6. **Open a pull request** against `main` in the upstream repository.

## Development Setup

Refer to the [README](README.md) for full setup instructions.

**Quick start:**

- Go 1.23 or later
- PostgreSQL running locally
- Install dependencies and start the dev server:

```bash
make dev
```

## Commit Conventions

Use conventional commit prefixes for all commit messages:

| Prefix       | Usage                                      |
| ------------ | ------------------------------------------ |
| `feat:`      | New feature                                |
| `fix:`       | Bug fix                                    |
| `docs:`      | Documentation changes                      |
| `refactor:`  | Code restructuring without behavior change |
| `test:`      | Adding or updating tests                   |
| `chore:`     | Build, CI, dependency, or tooling changes  |

**Examples:**

```
feat: add TOTP two-factor authentication
fix: resolve redirect loop on expired sessions
docs: update deployment instructions for Docker
refactor: extract notification dispatch into service layer
```

Keep the subject line under 72 characters. Add a body for additional context when necessary.

## Pull Request Process

1. **Title and description** -- Write a clear, concise PR title. Include a description of what changed and why.
2. **Link related issues** -- Reference issues with `Closes #123` or `Relates to #456`.
3. **CI must pass** -- All automated checks must be green before review.
4. **Maintainer review required** -- At least one maintainer must approve the PR before it can be merged.
5. **Squash-merge preferred** -- PRs are squash-merged to keep the commit history clean. Ensure your PR title is suitable as a final commit message.
6. **Respond to feedback** -- Address review comments promptly. Push additional commits rather than force-pushing, so reviewers can see incremental changes.

## Questions

If you have questions about contributing, open a [discussion](../../discussions) or reach out in an existing issue thread. We are happy to help you get started.
