# Milestone 1 — Chunk 1: Licensing & Compliance

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Establish the legal foundation for Cloudzilla by adding a BSL 1.1 license, contributor guidelines with CLA requirement, and a security vulnerability reporting policy.

**License model:** Business Source License 1.1 (BSL 1.1). Each tagged release starts its own 4-year timer, after which that release converts to Apache 2.0. The Additional Use Grant permits self-hosting for internal use but prohibits offering Cloudzilla as a commercial hosted/managed service.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `LICENSE` | Create | BSL 1.1 full license text with Cloudzilla-specific parameters |
| `CONTRIBUTING.md` | Create | Contributor guide: CLA, workflow, conventions, licensing terms |
| `SECURITY.md` | Create | Vulnerability disclosure policy and process |
| `.github/CLA.md` | Create | CLA text that CLA Assistant will present to contributors |
| `README.md` | Edit | Add "License" section at the bottom referencing BSL 1.1 |

---

## Task 1: Create the LICENSE File

- [ ] Create file `LICENSE` in the repository root with the full BSL 1.1 text.

Use the official MariaDB BSL 1.1 template with these parameters:

```
Business Source License 1.1

Parameters

Licensor:             Cloudzilla

Licensed Work:        Cloudzilla
                      The Licensed Work is (c) 2026 Cloudzilla.

Additional Use Grant: You may make use of the Licensed Work, provided that
                      you do not use the Licensed Work for a Commercial
                      Hosted Service.

                      A "Commercial Hosted Service" means offering the
                      Licensed Work to third parties as a managed service,
                      hosted platform, or software-as-a-service where the
                      service provides substantially the same functionality
                      as the Licensed Work.

                      Self-hosting the Licensed Work for your own internal
                      use (including use within your organization) is
                      permitted.

Change Date:          Per-release. Each tagged release of the Licensed Work
                      converts to the Change License four (4) years after
                      its release date. For example, a version released on
                      2026-04-15 converts to the Change License on
                      2030-04-15.

Change License:       Apache License, Version 2.0

For information about alternative licensing arrangements for the Licensed
Work, please contact: licensing@cloudzilla.dev
```

Then include the full BSL 1.1 license body text below the parameters block.

**Verification:** Confirm the file has the five required BSL 1.1 parameter fields (Licensor, Licensed Work, Additional Use Grant, Change Date, Change License) plus the full license terms.

---

## Task 2: Create CONTRIBUTING.md

- [ ] Create file `CONTRIBUTING.md` in the repository root.

Must include:
- CLA requirement (CLA Assistant GitHub App — sign on first PR)
- Code of conduct statement
- Fork → branch → PR workflow
- Dev setup reference (link to README)
- Commit message conventions (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`)
- PR process (CI checks, maintainer review, squash-merge preferred)
- Statement that contributions are licensed under BSL 1.1

**Verification:** Confirm CLA, code of conduct, workflow, commit conventions, PR process, and BSL 1.1 licensing are all mentioned.

---

## Task 3: Create SECURITY.md

- [ ] Create file `SECURITY.md` in the repository root.

Must include:
- Email-based reporting: **security@cloudzilla.dev** (not public issues)
- Response timeline: acknowledgment within 48 hours, assessment within 1 week
- Supported versions table (latest = yes, previous minor = security fixes only)
- Coordinated disclosure policy
- In-scope: server app, auth bypass, SQLi, XSS, CSRF, git transport, SSH
- Out-of-scope: DoS, social engineering, third-party deps, misconfigurations

**Verification:** Email reporting, response times, supported versions, and disclosure policy are present.

---

## Task 4: Create .github/CLA.md

- [ ] Create file `.github/CLA.md` with the CLA text.

The CLA must grant Cloudzilla:
- Perpetual, worldwide, non-exclusive, royalty-free, irrevocable license
- Right to reproduce, modify, display, distribute, sublicense
- Right to **re-license under any license, including proprietary**

Contributor represents:
- Contribution is original work
- Has legal authority to grant license
- Does not violate third-party rights

**Verification:** CLA grants full commercial rights (sublicense, re-license including proprietary), which is necessary for BSL 1.1 dual-licensing.

---

## Task 5: Update README.md with License Section

- [ ] Add a "License" section at the bottom of `README.md`:

```markdown
## License

Cloudzilla is licensed under the [Business Source License 1.1](LICENSE).

- **Self-hosting for internal use is permitted.**
- Offering Cloudzilla as a commercial hosted or managed service is prohibited.
- Each release converts to [Apache 2.0](https://www.apache.org/licenses/LICENSE-2.0) four years after its release date.

See [LICENSE](LICENSE) for the full terms.
```

---

## Task 6: Source File License Headers

**Decision: Do NOT add per-file license headers.**

Rationale:
- 90+ `.go` files and 85+ `.templ` files, none with headers currently
- Root `LICENSE` applies to entire repository — per-file headers not legally required
- Headers create maintenance burden (new files, sync)
- Industry precedent: CockroachDB, Sentry use root LICENSE only

No action required.

---

## Task 7: Configure CLA Assistant (Manual)

- [ ] Install [CLA Assistant GitHub App](https://github.com/apps/cla-assistant) on the repository
- [ ] Configure it to point to `.github/CLA.md`
- [ ] App will auto-comment on PRs from first-time contributors

**Note:** Requires GitHub repo admin access — cannot be done via code.

---

## Verification Checklist

- [ ] `LICENSE` exists with BSL 1.1 and correct parameters
- [ ] `CONTRIBUTING.md` references CLA, BSL 1.1, and contribution workflow
- [ ] `SECURITY.md` has email reporting and response timelines
- [ ] `.github/CLA.md` has full CLA with commercial re-licensing rights
- [ ] `README.md` has License section
- [ ] No per-file license headers (intentional)
- [ ] All files use LF line endings with trailing newline
