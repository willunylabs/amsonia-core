# Core role-member validation — 2026-11-03

Implements issue #65. Reused the existing checkout, Node dependencies and Go
caches. Native PostgreSQL 16 used a task-owned disposable cluster with separate
integration and browser databases. Runtime roles were non-owner, non-superuser
and NOBYPASSRLS, configured through the repository's actual role-grant script.

## Automated checks

- Go formatting, vet, unit and race tests; reachable-symbol vulnerability scan.
- Full native PostgreSQL race suite for `postgres` and `internal/coreapp`.
- Both source-provenance checks and the product-identifier check.
- TypeScript, ESLint, 45 Vitest tests, production build and npm audit.
- Redocly OpenAPI lint and parsing of the new role-member endpoint.

New database coverage uses 35 assigned and 31 available members. It verifies
cursor traversal, matching totals, active/suspended filters, literal search,
role/view/tenant cursor boundaries, missing and retired roles, both read
permissions, read-only inspection, insufficient delegation coverage, suspended
assignment rejection, version conflicts, removal of suspended bindings, the
last-administrator guard, clearing bindings and grants before retirement, and
successful unassignment audit records. Default HTTP role detail and the public
Service detail contract still return complete subject-ID arrays. Explicit
summary reads return the complete count with an empty array.

New unit coverage round-trips the maximum valid IDs/query with JSON escaping;
directory cursors now accept up to 8192 bytes. Component tests verify scoped
search/paging, discarded responses after view switching, disabled assignment to
suspended members, no member requests without member-read access, read-only
controls and deliberate retry with a fresh role version after a conflict. API
tests verify summary/history query parameters and encoded role-member filters.
Existing session and tenant-reconciliation tests remain green.

## Real browser checks

At 1440×900: sign-in and tenant creation; 30-row and five-row assigned pages;
searching and removing a suspended member's binding; available-member search;
disabled suspended assignment; assigning an active member; correct updated
binding counts and retirement remaining disabled while assigned. Captured role
summary responses contained counts 35/34 and zero subject IDs. The role window
was widened to 860 px to fit filters and actions; its member table has a bounded
scroll area.

At 390×844: created an empty temporary role, assigned a member, verified
retirement was disabled, removed the binding, then confirmed retirement. The
member's separate Support binding remained. The retired role disappeared from
the role directory. The 358 px window and document had no horizontal overflow;
the member table scrolled locally. Escape closed the role window and restored
focus to its opening button. Completed desktop/mobile flows had no console/page
errors or failing HTTP responses.

Screenshots are retained in `output/core-role-members-desktop.png` and
`output/core-role-members-mobile.png`. Logs and browser traces are retained in
`output/core-role-members-validation/`; these are evidence rather than caches.

Kernel decision/mutation APIs, authorization rules, provenance-owned source,
dependencies and database migrations were unchanged. The new member directory
requires role-read and member-read grants; binding writes reuse the existing
versioned Manager path and its coverage, audit and administrator protections.

## Cleanup

Closed this task's isolated browser context, API, Vite and PostgreSQL instance.
Preserved logs/screenshots before deleting the stopped 103 MiB disposable
cluster/binary directory, generated `web/dist` and this run's application
TypeScript build metadata. Existing dependencies and older Node build metadata
were retained. The Data volume had 67 GiB available after cleanup.
