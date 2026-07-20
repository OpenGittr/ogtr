# Security Policy

## Supported versions

ogtr is pre-1.0: only the **latest release** (and the current `main`
branch) receives security fixes. Older tags are not patched — upgrade to
the latest release to stay covered.

## Reporting a vulnerability

Please report vulnerabilities **privately** — do not open a public issue
for anything exploitable.

- Preferred: the repository's private **GitHub security advisory** ("Report
  a vulnerability" under the Security tab), which reaches the maintainers
  directly.
- Alternatively, email the maintainers. For a specific *deployment* of
  ogtr, the right address is that deployment's configured
  `ABUSE_CONTACT` (also shown on its disabled-link and preview pages).

Include what you can: affected endpoint or component, reproduction steps,
impact, and any suggested fix. You can expect an acknowledgment within a
few days; we will coordinate a fix and disclosure timeline with you before
anything is published.

There is **no bug bounty program** — this is an open-source project;
reports are credited in the release notes unless you prefer otherwise.

## Reporting abusive links

A short link hosted *on* an ogtr deployment (phishing, malware, spam) is
an abuse report, not a vulnerability: use the deployment's own reporting
surface — append `+` to the short URL for the preview page and its "Report
this link" form (`POST /api/v1/report`), or write to the deployment's
`ABUSE_CONTACT`.

## Deployment hardening

Operator-facing security configuration — blocklist feeds, Google Web Risk,
rate limits, reserved aliases, the abuse contact — is documented in
[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) ("Abuse protection") and
summarized in the README's "Security-relevant configuration" section.
Never enable the `dev` auth provider in production.
