# Security Policy

> Cloudzilla is currently in **alpha**. We take security seriously at every stage of development and appreciate responsible disclosure of vulnerabilities.

## Reporting a Vulnerability

You can report security vulnerabilities by opening a [GitHub issue](https://github.com/mkappworks-dev/cloudzilla-app/issues).

> **Note:** Cloudzilla is not yet hosted as a production service. Public issue reporting is acceptable during this stage. Once Cloudzilla is available as a hosted service, we will switch to private disclosure only.

Include as much of the following as possible:

- Description of the vulnerability
- Steps to reproduce or proof-of-concept
- Affected version(s) or commit SHA
- Potential impact assessment
- Any suggested fix (optional)

## Response Timeline

| Stage              | Timeframe             |
| ------------------ | --------------------- |
| Acknowledgment     | Within 48 hours       |
| Initial assessment | Within 1 week         |
| Fix development    | Depends on severity   |
| Public disclosure  | After fix is released |

We will keep you informed of progress throughout the process.

## Supported Versions

| Version        | Supported                   |
| -------------- | --------------------------- |
| Latest release | Yes — full security support |
| Previous minor | Security fixes only         |
| Older versions | No                          |

> **Note:** Cloudzilla is in alpha. All users are encouraged to run the latest release at all times.

## Coordinated Disclosure Policy

We follow a coordinated disclosure process:

- We work directly with reporters to understand and validate the vulnerability.
- We develop and test a fix before any public disclosure.
- We credit reporters in the security advisory (unless anonymity is requested).
- We aim to release a fix within 30 days of a confirmed report.
- We ask that reporters do not publish details before a fix is available.

## Scope

### In Scope

The following areas are eligible for responsible disclosure:

- Cloudzilla server application
- Authentication and authorization bypass
- SQL injection (SQLi)
- Cross-site scripting (XSS)
- Cross-site request forgery (CSRF)
- Git transport vulnerabilities (HTTP smart protocol, SSH)
- SSH server vulnerabilities
- Privilege escalation (instance, org, or repo level)

### Out of Scope

The following are **not** in scope for this policy:

- Denial of Service (DoS) attacks
- Social engineering (phishing, etc.)
- Vulnerabilities in third-party dependencies (please report these upstream)
- Security issues arising from self-hosted misconfigurations
