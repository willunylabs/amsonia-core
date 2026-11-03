# Audit range and CSV export validation — 2026-11-03

Implemented against [issue #67](https://github.com/willunylabs/amsonia-core/issues/67).
No kernel contract, migration, database grant, dependency or source-owned file
changed. Existing audit list defaults and ID cursors remain compatible.

- `make go-check`: formatting, vet, unit and race tests passed. Govulncheck found
  no reachable vulnerabilities (four existing findings in required modules are
  outside imported packages/call paths).
- `go test -race -tags postgres -count=1 -p 1 ./postgres/ ./internal/coreapp/`
  passed against a disposable native PostgreSQL 16 cluster with the repository's
  configured non-superuser runtime role. New coverage includes inclusive start /
  exclusive end with timezone offsets, paging/export alignment, invalid dates,
  tenant boundaries, missing audit permission, empty CSV, Unicode and formula
  guards, exact 10,000-row acceptance, row/byte overflow as JSON before download
  headers, and a deterministic concurrent-append snapshot barrier.
- Console TypeScript, ESLint, 55 Vitest tests, production build, and npm audit
  passed. New client coverage includes applied date filters across pages, binary
  decoding after session refresh, account-change replay rejection, structured
  JSON errors, cancellation, obsolete tenant responses, Blob URL cleanup,
  permission loss, query retry, and invalid ranges.
- Redocly OpenAPI lint, both source provenance manifests, product identifiers and
  `git diff --check` passed.

Real browser checks used an isolated context, a separate disposable database and
the actual runtime database grants. At 1440×900 and 390×844, a date/operation/
outcome query displayed 30 then 5 events. Exporting from the older page downloaded
all 35 matching rows at both sizes, with 13 columns in descending ID order.
Parsed downloads retained Unicode, quotes and embedded newlines, used UTC times,
and contained tab-prefixed formula-like values. Events before the start, exactly
at the end, and with another outcome were excluded. An invalid range sent no API
request, and an empty query downloaded only the CSV header. Final audit workflows
produced no browser console errors or horizontal page overflow. Screenshots were
visually inspected.

Screenshots are retained at `output/core-audit-{desktop,mobile}.png`; downloaded
CSV fixtures, browser traces and validation logs at `output/core-audit-validation/`.
These ignored files are evidence, not source dependencies. The test-only initial
API signing parameter was corrected before the successful browser checks. Owned
browser contexts, API/Vite servers and PostgreSQL cluster are stopped at cleanup;
disposable databases, binaries and intermediate web build files are removed.
