# Session and tenant recovery validation — 2026-11-03

Issues #59 and #60. Reused the current checkout and installed dependencies. Native
PostgreSQL 16 ran in a task-owned disposable cluster; browser data used a separate
database from the integration-test target and a non-bypass runtime role.

- Complete Go format/vet/unit/race/vulnerability checks.
- Real PostgreSQL integration tests, including device-family logout after multiple
  rotations, cookie-only optional startup, account-wide rotated-token replay
  response, refresh service failures preserving cookies, and concurrent
  refresh/logout without restoring revoked credentials or revoking other devices.
- Migration 000007: deterministic signature-verification fault injection proves
  SQLSTATE 40001/40P01 propagate from both signed context readers. Unrelated
  verification errors return NULL. Parallel tenant invitation writes retain
  serialization errors. Existing forged-context and cross-tenant RLS tests remain.
- Frontend TypeScript/ESLint, 31 tests, production build, dependency audit, OpenAPI
  lint, product identifiers, and both provenance manifests.

Browser checks at `http://127.0.0.1:13039/` used 1440×900 and 390×844:

- Create first and additional tenants; select and remember the current tenant.
- Two concurrently opened tabs start without sessionStorage credentials and
  restore using the shared HttpOnly cookie. They remain one device session.
- A failed authentication API request displays retry while retaining the access
  token; restoring connectivity and retrying succeeds. The failure was injected
  at the API request, with page assets still available.
- Accept another invitation through a fragment change in an already open console.
- Remove the current account from one tenant and fall back to its remaining
  tenant. Remove the final membership, show no-tenant state, and still load account
  settings.
- Sign out after another tab rotated the cookie; an earlier access token returns
  401, local credentials clear, and an independent device stays signed in.
- Desktop/mobile header controls and page content have no horizontal overflow.

Screenshots are retained in `output/core-session-{desktop,mobile}.png`; logs and
browser traces are retained in `output/core-session-validation/`. Deliberate
network failure, expired-token probes, and concurrency conflicts are distinguished
from unexpected application errors. No uncaught application exceptions occurred.
Task-owned tabs and services are closed and temporary databases/binaries removed
at task close. Existing dependency caches and test evidence are retained.
