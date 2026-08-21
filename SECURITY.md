# Security Policy

## Reporting a vulnerability

Do **not** open a public issue for security vulnerabilities. Report them
privately to the maintainers.

- GitHub Security Advisory: https://github.com/willunylabs/amsonia-core/security/advisories/new
- Email: security@willuny.com (PGP fingerprint available on request)

You will receive an acknowledgment within 72 hours and a status update at least
every 5 business days until resolution.

## Scope

In scope:

- authorization decision correctness (allow/deny semantics);
- tenant isolation and RLS enforcement;
- delegated-administration and grant-cycle protection;
- immutable role version and audit integrity;
- secrets or credentials accidentally committed to this repository;
- session rotation, account bootstrap, and authentication lockout correctness;

Out of scope:

- Amsonia Source Distribution, the commercial SaaS source product;
- authentication systems outside the standalone Core API;

## Disclosure policy

We follow a 90-day coordinated disclosure window from initial report.
