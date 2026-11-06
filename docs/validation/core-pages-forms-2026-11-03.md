# Dedicated administration pages and forms — 2026-11-03

Implemented against [issue #69](https://github.com/willunylabs/amsonia-core/issues/69).
Tenant semantics, kernel contracts, migrations, database grants and dependencies
are unchanged. This iteration separates existing administration tasks into
addressable pages and adds complete member/password form flows.

- `make go-check` passed: formatting, vet, unit and race tests. Govulncheck found
  no reachable vulnerabilities; four existing required-module findings remain
  outside imported packages/call paths.
- Native PostgreSQL 16 `go test -race -tags postgres -count=1 -p 1 ./postgres/
  ./internal/coreapp/` passed with configured non-superuser runtime roles. New
  coverage verifies exact member reads, active/suspended targets, absent and
  out-of-tenant targets, caller membership and permission denials, atomic
  other-device revocation, retained current family, account isolation, idempotence,
  invalid/expired callers, concurrent refresh, and authentication revalidation
  after acquiring the account lock.
- TypeScript, ESLint without warnings, 73 Vitest tests, production build, npm
  audit, Redocly OpenAPI lint, both provenance checks, product identifiers and
  `git diff --check` passed. New client coverage checks separate account/role/
  member/invitation pages, encoded role IDs, grant submission/version handling,
  full-page cancel, exact removal confirmation, direct-address permission gates,
  password confirmation and obsolete form completion after account changes.

Real browser verification used two isolated contexts and a separate disposable
database on `http://127.0.0.1:3375`. Desktop 1440×900 checks covered full-page
tenant/role creation, role detail editing, separate grant creation, history
reload/back/forward, invitation creation and acceptance, exact member forms,
suspension persisted through reload, resume and role assignment. The invitation
directory is absent from Members; unrelated role and account forms are absent
from their sibling pages. A member without role-read access sees a clear denial
while retaining access to their own profile.

At 390×844, member forms, role members, account devices and password confirmation
were exercised. A long email heading initially overflowed; wrapping was fixed
and the final member/password/role-member pages have no horizontal page overflow.
Mobile navigation has distinct icons and short visible labels. The role-member
table retains its own horizontal scrolling. Password mismatch blocks submission;
a matching password change signs out and permits sign-in using the new password.
The dedicated Devices page signs out another isolated browser while preserving
the caller; the other browser returns to sign-in after reload. Final successful
flows reported no page errors or unexpected console errors. Expected revoked
session denials and intermediate Vite reload errors are retained in raw traces.

Visually inspected screenshots are retained at `output/core-pages-*.png`.
Validation logs and browser traces are in `output/core-devices-validation/`.
These ignored files are evidence. Owned browser contexts, API/Vite servers and
the PostgreSQL cluster are closed at cleanup; disposable databases, binaries and
web build intermediates are removed. Shared dependencies and caches are retained.
