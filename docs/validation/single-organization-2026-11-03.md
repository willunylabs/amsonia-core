# Single-organization default — 2026-11-03

Issue: #71. The standalone API and CLI default to single mode; embedded kernel
contracts retain their tenant behavior. Migration 8 adds only a narrow
organization-ID resolver. Runtime receives EXECUTE without global tenant SELECT.

## Automated validation

- Go formatting, vet, unit tests and race tests passed. The initial govulncheck
  network request stalled; retrying that check with HTTP/2 disabled completed:
  zero reachable vulnerabilities, four required-module-only findings.
- Full PostgreSQL 16 integration suite passed with race detection using actual
  runtime and maintenance grants. Added tests cover default bootstrap,
  repeated setup, concurrent recovery with a one-connection service pool,
  ambiguous/missing selection, membership enforcement, disabled tenant creation,
  tenant URL/authorization-check bypass attempts and disabled invitation
  preview/acceptance. Rejected links remain unconsumed. Multi mode restores the
  existing access workflow.
- Frontend typecheck, lint, 75 tests and production build passed. npm audit
  reported zero vulnerabilities. Mode-specific tests cover direct creation
  routes, hidden controls and account settings without organization membership.
- OpenAPI, product identifiers and server/web source provenance passed.
- Compose YAML parsed and includes the three new runtime settings. Docker is
  unavailable on this host; container execution was not tested locally.

## Native application and browser validation

An isolated temporary PostgreSQL cluster, runtime role and database were used.
The actual interactive CLI created an administrator and `Example Studio`.
Repeated `setup-organization` returned the same organization. The API was also
started before bootstrap, matching the local Compose startup order.

At 1440×900, default mode opened directly into the organization with no tenant
creation button or switcher. `#/tenants/new` rejected the workflow. An invitation
was created through its dedicated page and accepted in a separate browser
context. A role was created and assigned through the member's Roles page.
The joined member could then open the permitted member directory. Profile
remained independently accessible.

At 390×844, overview, member directory and member role assignment had no
horizontal overflow. Saved assignment state was verified. Screenshots were
inspected. The existing fixed mobile navigation remains reachable.

Restarting the same database in explicit multi mode exposed tenant creation.
A second tenant was created and switching between both organizations succeeded
without page errors. A separate single-mode API startup against that now
ambiguous database failed with the expected configuration error.

Local logs and screenshots are retained under the ignored
`output/core-organization-validation/` directory. Temporary databases, API/Vite
processes, browser contexts and build intermediates are task-owned and removed
after validation. Existing dependencies and shared caches are retained.
