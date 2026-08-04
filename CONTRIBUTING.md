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

```bash
go test ./...
go test -race ./...
go vet ./...
```

PostgreSQL integration tests run only with the `postgres` build tag:

```bash
TEST_DATABASE_ADMIN_URL=postgres://... TEST_DATABASE_URL=postgres://... \
  go test -tags postgres -count=1 ./postgres/
```

The test applies the reference migration to the target database and truncates
all Amsonia tables. Use an isolated disposable database, never production.

## Reporting bugs

Open an issue with a minimal reproduction, the Amsonia version, and the expected
versus actual decision. Security issues follow [SECURITY.md](SECURITY.md).

## Pull requests

1. Open an issue describing the change unless it is a trivial fix.
2. Implement with tests demonstrating the behavior change.
3. Run the checks above plus `gofmt -l .` (must be empty).
4. Request review from a maintainer.
