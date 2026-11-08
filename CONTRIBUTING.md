# Contributing

Thanks for your interest in Amsonia.

## Ground rules

- Amsonia is a security-sensitive authorization kernel. Changes to decision
  semantics, scope evaluation, delegation, or audit behavior require review by
  the maintainers; do not submit them without an issue first.
- The public API is the compatibility contract. Renaming exported symbols,
  changing signatures, or adding exported fields requires a design revision
  and a new release plan.
- No hidden telemetry, phone-home behavior, or required cloud account may be
  introduced.
- All new code must be Go-idiomatic, formatted with `gofmt`, and covered by
  deterministic tests.

## Development

From a fresh checkout with Go 1.26.10 or newer and Node.js 22.19 or newer,
run `make check` to exercise the Go, web, site, and OpenAPI checks.
The web and site targets supply the same test configuration used in CI.
For Go-only changes, the following checks can run separately:

```bash
go test ./...
go test -race ./...
go vet ./...
```

PostgreSQL integration tests run only with the `postgres` build tag:

```bash
TEST_DATABASE_ADMIN_URL='postgres://db_owner@127.0.0.1:5432/amsonia_test?sslmode=disable' \
  make postgres-check
```

The test applies the reference migration to the target database and truncates
all Amsonia tables. Use an isolated disposable database, never production.

## Reporting bugs

Use the [GitHub issue chooser](https://github.com/willunylabs/amsonia-core/issues/new/choose)
to report a bug or discuss an improvement. Include the exact release or commit,
your installation method, and steps that reproduce the behavior. Report security
issues through the private channels listed in [SECURITY.md](SECURITY.md).

## Pull requests

1. Open an issue describing the change unless it is a trivial fix.
2. Implement with tests demonstrating the behavior change.
3. Run the checks above plus `gofmt -l .` (must be empty).
4. Request review from a maintainer.
