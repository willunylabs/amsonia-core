# Amsonia Core

Amsonia Core is an Apache-2.0 full-stack foundation for multi-tenant Go SaaS
products. It combines a Go authorization kernel, PostgreSQL persistence with
forced row-level security, a versioned HTTP API, and a React management console
in one repository.

Core focuses on identity, sessions, tenants, memberships, roles, permissions,
authorization decisions, and policy audit. Billing, commerce, AI, education,
messaging, and white-label operations belong to the complete Amsonia product,
not this open-source core.

## What it provides

- Tenant-first delegated authorization with `own`, `workspace`, and `tenant`
  scopes.
- PostgreSQL-enforced tenant isolation with signed transaction-local bindings
  and `FORCE ROW LEVEL SECURITY`.
- Argon2id administrator authentication, lockout, rotating refresh sessions,
  and one-time administrator bootstrap.
- Permission catalogs, immutable role versions, grant-cycle protection, and
  append-only audit events.
- A standalone Go API, migration/administration CLI, in-memory adapter, and
  React management console.

## Repository layout

| Path | Purpose |
| --- | --- |
| `cmd/api` | Standalone HTTP API |
| `cmd/amsonia` | Migration and administrator CLI |
| `memory` | In-memory adapter for tests |
| `postgres` | PostgreSQL adapter, migrations, and RLS tests |
| `internal/coreapp` | Identity, session, tenant, and HTTP composition |
| `web` | React/Vite management console |
| `openapi` | Versioned HTTP contract |
| `ops` | Least-privileged database role provisioning |

## Quick start

Requirements: Go 1.25.13+, PostgreSQL 16+, Node.js 22.15+, and npm 10.
Docker is optional. The complete local setup, role provisioning, environment
variables, and PostgreSQL checks are in
[docs/getting-started.md](docs/getting-started.md).

After completing the database and environment setup, the shortest development
flow is:

```bash
go run ./cmd/amsonia migrate
go run ./cmd/amsonia bootstrap-admin
go run ./cmd/api
```

In another terminal:

```bash
npm --prefix web ci
npm --prefix web run dev
```

Then open `http://127.0.0.1:3000` and sign in with the bootstrap administrator.

## HTTP API

The versioned contract is [openapi/openapi.yaml](openapi/openapi.yaml). Core
routes cover:

- authentication and session refresh;
- tenants and memberships;
- permission catalogs and roles;
- audit events; and
- authorization checks.

The API is intended to sit behind HTTPS in production. Refresh credentials are
HttpOnly, `SameSite=Strict` cookies and are never returned in JSON.

## Development

Run the local quality gate with:

```bash
make check
```

It covers Go format/vet/unit/race checks, the reachable-vulnerability scan,
web checks, and OpenAPI validation. Contribution rules are in
[CONTRIBUTING.md](CONTRIBUTING.md), and the security model/reporting process is
in [SECURITY.md](SECURITY.md) and [docs/security-model.md](docs/security-model.md).

## Amsonia product

Amsonia Core is the reusable open-source authorization foundation. The complete
Amsonia product adds production SaaS modules, managed upgrades, and broader
operations tooling. See [willuny.xyz](https://willuny.xyz) for the product and
architecture context:

- [Go SaaS boilerplate source kit](https://willuny.xyz/go-saas-boilerplate)
- [Multi-tenant SaaS architecture](https://willuny.xyz/architecture)
- [Multi-tenant foundations](https://willuny.xyz/features/multi-tenancy)
- [Source package options](https://willuny.xyz/shop/products)

## License

Amsonia Core is Apache-2.0. The complete Amsonia product and modules outside
this repository are licensed separately.
