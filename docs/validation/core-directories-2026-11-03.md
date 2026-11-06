# Core directory validation — 2026-11-03

Implements issue #63. Reused the existing checkout and dependency caches. Native
PostgreSQL 16 ran in a task-owned disposable cluster, with separate integration
and browser databases. Runtime roles were non-owner, non-superuser and
NOBYPASSRLS, configured by the repository's actual role-grant script.

## Automated checks

- Go formatting, vet, unit and race tests; reachable-symbol vulnerability scan.
- Both exported-source provenance checks and product-identifier checks.
- TypeScript, ESLint, 38 Vitest tests, production build and npm audit.
- Redocly OpenAPI validation.
- Native PostgreSQL race suite for `postgres` and `internal/coreapp`.
- New database coverage: 66 members, 66 live roles and 245 invitations; traversal
  without duplicate/skipped IDs; equal invitation timestamps; filtered totals;
  literal percent/underscore and Unicode queries; all invitation statuses;
  role renames and newer invitation insertion between pages; invalid limits and
  cursors; foreign tenant/filter cursors; permission denial including totals;
  cross-tenant membership denial under the real signed RLS boundary.
- New browser-component coverage: filters on previous/next pages; obsolete
  response rejection; failed-page retry; an emptied final page returning to the
  same search's beginning; older invitation revocation; assigned roles across
  pages; displayed role version on writes; retaining the role page after saving;
  no role directory requests without read authority. Existing cookie/session and
  tenant-reconciliation regressions remain covered.

## Real browser checks

At 1440×900: sign-in and tenant creation; active-member overview count 61 and
live-role count 66; bounded 30-row member pages with previous/next; searching and
revoking `invite_003`, beyond the former latest-200 boundary; role assignment
from the third role page; retained assignments during a different search;
literal `%_` role search; role detail binding count and disabled retirement while
assigned. Explicit status-selector accessible names were added during browser
verification.

At 390×844: member status filtering and resume; filtered member count changed
from five to four; pending invitation search retained across both 30-row pages,
with the final next button disabled. Both viewports had no document-level
horizontal overflow. The member table retained its local horizontal scrolling.
Console/page errors and failing HTTP responses were absent during the completed
desktop and mobile flows.

Screenshots are retained locally in `output/core-directory-desktop.png` and
`output/core-directory-mobile.png`. Validation logs and browser traces are
retained in `output/core-directory-validation/`. These are evidence, not caches.

The HTTP collection contract is now bounded by default; API consumers must
follow `next_cursor` with unchanged search/status filters for a complete list.
Pages are live snapshots. No kernel public API, authorization semantics,
provenance-owned source, dependency or database migration was changed.

## Cleanup

Closed the task-owned browser tab, API, Vite server and PostgreSQL cluster.
Preserved screenshots, logs and browser traces before removing the disposable
102 MiB database/binary directory, generated `web/dist` and this run's application
TypeScript build metadata. Existing dependency caches, older Node build metadata
and all source were retained. Available Data-volume space after cleanup: 66 GiB.
