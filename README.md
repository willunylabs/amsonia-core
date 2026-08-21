# Amsonia Core — Tenant authorization for Go, enforced in PostgreSQL

[![Apache-2.0 license](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![CI](https://github.com/willunylabs/amsonia-core/actions/workflows/ci.yml/badge.svg)](https://github.com/willunylabs/amsonia-core/actions/workflows/ci.yml)

**Amsonia Core** is a self-hosted authorization foundation for Go teams building
multi-tenant SaaS. It keeps tenant membership, delegated RBAC, administrator
sessions, policy audit, and PostgreSQL row-level isolation in one source-owned
system instead of leaving those boundaries to every handler.

[Visit Amsonia](https://amsonia.dev) · [Try the live demo](https://demo.amsonia.dev) · [Read about Amsonia Source Distribution](https://willuny.com/amsonia) · [View the API contract](openapi/openapi.yaml)

The demo is a read-only technical preview of the authorization console. The
repository is the product: the API, migrations, policy kernel, integration
tests, OpenAPI contract, and Console are all inspectable.

Core focuses on identity, sessions, tenants, memberships, roles, permissions,
authorization decisions, and policy audit. Billing, commerce, AI, education,
messaging, and white-label operations belong to Amsonia Source Distribution,
not this open-source Core.

## The problem it solves

Multi-tenant authorization usually begins as scattered `tenant_id` filters and
role checks. The expensive failures arrive later: one missing predicate exposes
another customer's row, delegated admins create privilege cycles, policy
changes cannot be reconstructed, and identity/session code grows beside the
authorization code without a stable boundary.

Core makes those invariants explicit:

- **Storage isolation:** a signed transaction-local tenant binding drives
  PostgreSQL `FORCE ROW LEVEL SECURITY`; forged settings reveal no tenant rows.
- **Delegation safety:** immutable role versions, scoped permissions, expected
  versions, and grant-cycle protection constrain administrative changes.
- **Operational evidence:** append-only audit events and a versioned OpenAPI
  contract make behavior inspectable rather than implicit.
- **Source ownership:** the Go kernel, adapters, schema, API, tests, and Console
  run in infrastructure you control.

## Fit

Use Core when you are building a Go/PostgreSQL multi-tenant product, want RBAC
and tenant isolation to share one tested boundary, and prefer operating source
code over depending on a hosted authorization control plane.

Core is not a general-purpose relationship graph, not an OAuth/OIDC identity
provider, and not a drop-in replacement for every policy engine. The host
application still authenticates end users, identifies the active tenant, loads
business resources, and decides which permission a request requires. Core
evaluates that permission and can enforce the same tenant boundary on
application-owned PostgreSQL tables.

| Approach | Best fit | Tenant data isolation | Operating shape |
| --- | --- | --- | --- |
| **Amsonia Core** | Go SaaS with tenant RBAC, admin sessions, audit, and PostgreSQL | Signed context plus reference `FORCE RLS` policies | Self-hosted source, API, CLI, Console |
| Embedded policy library | Application-specific RBAC/ABAC logic | Designed separately by the application | Linked into each service |
| Relationship/ReBAC service | Fine-grained object relationship graphs | Application database remains a separate boundary | Dedicated authorization service |
| Identity platform | Login, federation, and user lifecycle | Depends on the product and integration | Hosted or self-hosted identity service |

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

## Verify it in five minutes

Requirements: Docker with the Compose plugin and Go 1.25.13+.

```bash
git clone https://github.com/willunylabs/amsonia-core.git
cd amsonia-core
make demo
```

The command generates independent local infrastructure credentials in a
Git-ignored, owner-readable file, builds the complete stack, and prompts once
for an administrator password without echoing or storing it. Open
`http://127.0.0.1:8080`. Re-running `make demo` is idempotent; `make demo-down`
stops the stack without deleting its PostgreSQL volume.

Then run the host-table proof in
[`examples/business-data-rls`](examples/business-data-rls): two tenants query
the same invoice table without tenant predicates, each sees only its own row,
and a cross-tenant insert is rejected by PostgreSQL RLS. The walkthrough is in
[`docs/business-data-rls.md`](docs/business-data-rls.md).

## Evidence, not feature claims

| Invariant | Public evidence |
| --- | --- |
| Forged tenant settings expose no Core or host rows | [`postgres/store_integration_test.go`](postgres/store_integration_test.go), [`postgres/host_table_integration_test.go`](postgres/host_table_integration_test.go) |
| Runtime and maintenance roles cannot bypass RLS | [`ops/configure_database_roles.sql`](ops/configure_database_roles.sql) |
| Delegation cycles and stale role versions are rejected | Go unit tests and PostgreSQL lifecycle tests |
| Browser/API behavior stays versioned | [`openapi/openapi.yaml`](openapi/openapi.yaml) and Console contract tests |
| Identity credentials and sessions use hardened primitives | [`docs/security-model.md`](docs/security-model.md) and `internal/security` tests |

## Repository layout

| Path | Purpose |
| --- | --- |
| `cmd/api` | Standalone HTTP API |
| `cmd/amsonia` | Migration and administrator CLI |
| `memory` | In-memory adapter for tests |
| `postgres` | PostgreSQL adapter, migrations, and RLS tests |
| `internal/coreapp` | Identity, session, tenant, and HTTP composition |
| `web` | React/Vite management console |
| `site` | Astro product and technical website |
| `openapi` | Versioned HTTP contract |
| `ops` | Least-privileged database role provisioning |

## Manual development setup

Requirements: Go 1.25.13+, PostgreSQL 16+, Node.js 22.15+, and npm 10.
Docker is optional. The complete local setup, role provisioning, environment
variables, and PostgreSQL checks are in
[docs/getting-started.md](docs/getting-started.md).

Use this path when you want to manage PostgreSQL and Node.js directly rather
than use the five-minute Compose flow. After completing the database and
environment setup:

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

The project positioning and proof-first launch material are maintained in
[`docs/launch/launch-kit.md`](docs/launch/launch-kit.md). Contributions that
add a reproducible example, negative security test, integration guide, or
measured operational result are especially useful.

## Amsonia product

Amsonia is the product. Amsonia Core is its reusable open-source
authorization foundation. Amsonia Source Distribution is the complete
commercial Go + Next.js SaaS codebase published by Willuny Labs, adding
production SaaS modules, updates, and broader operations tooling. See
[willuny.com/amsonia](https://willuny.com/amsonia) for the product and architecture context:

- [Go SaaS boilerplate source kit](https://willuny.com/go-saas-boilerplate)
- [Multi-tenant SaaS architecture](https://willuny.com/architecture)
- [Multi-tenant foundations](https://willuny.com/features/multi-tenancy)
- [Source package options](https://willuny.com/shop/products)

## License

Amsonia Core is Apache-2.0. Amsonia Source Distribution and modules outside
this repository are licensed separately.
