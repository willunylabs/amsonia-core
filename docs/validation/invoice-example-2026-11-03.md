# Invoice form example validation

Issue #80. No default API/console routes, kernel contracts, migrations or release
bundle contents changed.

- Go format, vet, unit tests, race tests and vulnerability scan passed. The HTTP
  test covers authentication, read-only denial, cross-origin rejection,
  forged tenant fields, duplicate form fields, validation, redirect after
  save, and organization-scoped storage calls.
- With a native PostgreSQL 16 cluster, the original invoice proof passed
  using a non-owner runtime role. The tagged host-table integration test
  additionally checked forged settings and cross-tenant isolation.
- The opt-in server was verified at `http://127.0.0.1:56323/` at 1440×900 and
  390×844. The Acme editor saved an invoice; reloading the list did not
  resubmit. The Globex editor could not see the Acme invoice and saved a
  separate one. The Acme viewer saw Acme data, no Globex rows or create link,
  and received 403 on direct submission. No page errors, unexpected HTTP
  failures or mobile horizontal overflow occurred.
- Local screenshots and logs are retained under the ignored
  `output/invoice-example-validation/` directory.
