# Organization settings validation

Issue #73. Go format, vet, unit and race checks passed; govulncheck reported
zero reachable findings. The PostgreSQL 16 race suite verified authorization,
tenant selection, concurrent version checks, atomic audit rollback, and that
the runtime role cannot read or update the tenant table directly.

Frontend typecheck, lint, 83 tests and the production build passed. OpenAPI,
source provenance, product identifiers and npm audit passed.

The native API and Vite console were tested at
`http://127.0.0.1:3395/#/organization` (1440×900 and 390×844). Two independent
administrator sessions exercised a successful rename, an expected HTTP 409
with preserved draft, explicit reload, and a successful resubmission updating
the workspace header. A member could inspect the settings without editing
and navigate to their account profile. No page errors, unexpected API failures
or mobile horizontal overflow were observed. Screenshots are retained under
the ignored `output/core-settings-validation/` directory.
