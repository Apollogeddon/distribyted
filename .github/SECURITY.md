# Security policy

## Supported versions

Only the latest release of distribyted receives security fixes.

## Reporting a vulnerability

Report security vulnerabilities privately through [GitHub's private vulnerability reporting](https://github.com/Apollogeddon/distribyted/security/advisories/new). Don't open a public issue.

You can expect an initial response within a few days. If the issue is confirmed, the fix is released as a patch version and credited in the advisory unless you ask otherwise.

This is a fork of [distribyted/distribyted](https://github.com/distribyted/distribyted). A vulnerability that also affects the original project should be reported to it as well.

## Automated security tooling

This repository's CI runs on every pull request, on every push to `main` and weekly:

- **Gitleaks** scans for committed secrets.
- **govulncheck** reports known vulnerabilities in the Go code distribyted calls.
- **OSV-Scanner** reports known vulnerabilities in the Go modules.
- **CodeQL** analyses the Go code for security issues.

**Dependabot** proposes updates to Go modules, Docker images and GitHub Actions.
