# Core administration validation — 2026-11-03

Implemented against issue #57. Tested with native PostgreSQL 16 using a disposable
cluster and a non-superuser, non-bypass runtime role configured by the repository's
actual role-grant script. No production services were changed.

- `make go-check`: formatting, vet, unit tests, race tests, and reachable-symbol
  vulnerability scanning.
- `go test -race -tags postgres -count=1 -p 1 ./postgres/ ./internal/coreapp/`:
  migrations, signed RLS, invitation new/existing account acceptance, invalid/reused/
  reissued/revoked/expired links, read-only denial, cross-tenant denial, separate
  grant-management authority, suspended actor/removed target transaction checks,
  active last-administrator protection, role editing/grants/bindings/retirement,
  history and audit cursors, session refresh/revocation ownership, password changes,
  refresh-cookie clearing, and unbound SQL isolation.
- Console TypeScript, ESLint, Vitest, production build, and npm audit.
- Redocly OpenAPI lint, product-identifier checks, and both provenance manifests.

Real browser checks used separate administrator and invited-member contexts at
1440×900 and 390×844: sign-in, tenant creation, invitation creation and acceptance,
zero-permission access, role creation/assignment/edit/history, permission grant and
revoke, filtered audit details, member suspension/resume/removal, invitation
revocation, profile editing, current-device revocation, password change and new
password sign-in. No console errors or page-level horizontal overflow were found.
The member table scrolls within its own container on narrow screens.

Local screenshots are retained under `output/core-admin-{desktop,mobile,audit}.png`;
validation logs are retained under `output/core-admin-validation/`. These ignored
files are task evidence, not source dependencies. The temporary API, Vite server,
and PostgreSQL cluster are stopped and disposable binaries/database files are
removed after verification.

The new lifecycle regression also exercises the existing PostgreSQL adapter's
empty workspace-role constraint handling. Deletion now normalizes nil to the
same empty array used by insertion; otherwise a revoke could silently become a
no-op and the following versioned write would conflict.
