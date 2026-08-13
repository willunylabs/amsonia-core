# Amsonia Core

Amsonia Core is an Apache-2.0, full-stack foundation for tenant administration
and delegated authorization in Go SaaS products. The v0.2 preview ships the Go
kernel, PostgreSQL persistence with forced RLS, a standalone HTTP API and CLI,
and a focused React management console in one repository.

It is deliberately smaller than the complete Amsonia product: Core owns
identity, sessions, tenants, memberships, roles, permissions, authorization
checks, and policy audit. Billing, commerce, AI, education, messaging, and
white-label operations stay outside the open-source boundary.

## Why Amsonia

Most authorization systems either embed a generic policy engine or add a
separate relationship service. Amsonia takes a narrower position:

> Tenant-first delegated authorization, embedded directly in your Go SaaS.

- Every policy read and mutation has an explicit tenant boundary.
- PostgreSQL enforces that boundary again with signed, transaction-local
  bindings and `FORCE ROW LEVEL SECURITY`.
- Administrators can only delegate permissions and scopes they already hold.
- Role changes create immutable snapshots and append-only audit events.
- Last-administrator and grant-cycle guards fail closed inside the transaction.
- The API and React console are useful on their own; the Go kernel remains
  embeddable without the web application.

## Repository map

| Path | Purpose |
| --- | --- |
| `cmd/api` | Standalone HTTP API |
| `cmd/amsonia` | Migration and one-time administrator CLI |
| `memory` | Transactional in-memory adapter for tests and local embedding |
| `postgres` | PostgreSQL adapter, versioned migrations, and RLS tests |
| `internal/coreapp` | Core identity, session, tenant, and HTTP composition |
| `web` | React/Vite tenant management console |
| `ops` | Least-privileged database role provisioning |
| `openapi` | Versioned HTTP contract |

## Included in v0.2 preview

- Email/password administrator sign-in with Argon2id hashing and login lockout.
- Fifteen-minute access tokens plus rotating HttpOnly refresh cookies with
  replay-family revocation.
- One-time system-administrator bootstrap; there is no public registration.
- Tenant creation, active memberships, permission catalog, roles, and
  atomically assigned initial permissions.
- `own`, `workspace`, and `tenant` authorization scopes with stable decisions.
- Immutable role versions, mutation audit, grant-cycle protection, and
  last-administrator protection.
- Versioned migration runner with advisory locking and durable dirty markers.
- Separate runtime and maintenance PostgreSQL roles; neither may be superuser,
  table owner, or `BYPASSRLS`.
- Desktop and mobile management UI for overview, members, roles, permission
  catalog, audit history, and live policy checks.

Invitations and richer member/role editing are represented in the data model
but are not yet exposed as v0.2 preview UI workflows.

## Requirements

- Go 1.25.12 or newer within the supported release line
- PostgreSQL 16 or newer
- Node.js 22.15 or newer and npm 10

Docker is not required. The commands below use a local PostgreSQL instance.

## Local quick start (without Docker)

Create an empty database and two non-privileged login roles. Use your own role
passwords; do not reuse these names if they already belong to another service.

```bash
createdb amsonia_core
createuser --login --no-superuser --no-createdb --no-createrole --no-bypassrls --pwprompt amsonia_runtime
createuser --login --no-superuser --no-createdb --no-createrole --no-bypassrls --pwprompt amsonia_maintenance
```

Run all migrations as the database owner:

```bash
export AMSONIA_MIGRATION_DSN='postgres://db_owner@127.0.0.1:5432/amsonia_core?sslmode=disable'
go run ./cmd/amsonia migrate
go run ./cmd/amsonia migration-status
```

Install the least-privileged grants using a PostgreSQL cluster administrator
that is allowed to harden and revoke memberships from the two login roles.
This is deliberately a separate operator boundary from the normal migration
connection. The script prompts for two independent binding secrets as 64 or
more hexadecimal characters; generate each with `openssl rand -hex 32` and
paste it at the prompt.

```bash
export AMSONIA_ROLE_ADMIN_DSN='postgres://role_admin@127.0.0.1:5432/amsonia_core?sslmode=disable'
psql "$AMSONIA_ROLE_ADMIN_DSN" \
  -v runtime_role=amsonia_runtime \
  -v maintenance_role=amsonia_maintenance \
  -f ops/configure_database_roles.sql
```

Convert the runtime hex secret to unpadded base64url for the API. Keep the
result in a secret manager or local untracked `.env`, never in Git.

```bash
export AMSONIA_DATABASE_DSN='host=127.0.0.1 port=5432 dbname=amsonia_core user=amsonia_runtime sslmode=disable'
export PGPASSWORD='YOUR_RUNTIME_ROLE_PASSWORD'
export AMSONIA_TENANT_BINDING_SECRET='YOUR_UNPADDED_BASE64URL_SECRET'
```

Create the first administrator exactly once with a password between 12 and 128
Unicode characters, then start the API:

```bash
go run ./cmd/amsonia bootstrap-admin
go run ./cmd/api
```

In another terminal, start the console. Its default API proxy is
`http://127.0.0.1:8080`; override it when the API uses another port.

```bash
npm --prefix web ci
VITE_API_PROXY_TARGET='http://127.0.0.1:8080' npm --prefix web run dev
```

Open `http://127.0.0.1:3000` and sign in with the bootstrap administrator.

## Configuration

| Variable | Required | Meaning |
| --- | --- | --- |
| `AMSONIA_MIGRATION_DSN` | CLI migrate/status | Database-owner connection used only by the migration CLI |
| `AMSONIA_ROLE_ADMIN_DSN` | Role provisioning | Operator-only cluster role administrator; never passed to an application process |
| `AMSONIA_DATABASE_DSN` | API/bootstrap | Non-superuser runtime connection |
| `AMSONIA_TENANT_BINDING_SECRET` | API/bootstrap | Unpadded base64url of the runtime role's 32+ byte binding secret |
| `AMSONIA_HTTP_ADDR` | No | API listen address, default `:8080` |
| `VITE_API_PROXY_TARGET` | Web dev only | Local Vite proxy target, default `http://127.0.0.1:8080` |
| `VITE_API_URL` | Web build only | Optional same-origin path prefix; cross-origin API origins are rejected |

See [.env.example](.env.example) for names and safe placeholders.

## HTTP API

The versioned contract is [openapi/openapi.yaml](openapi/openapi.yaml). Primary
routes include:

- `GET /health` and `/readyz`
- `POST /api/v1/auth/login`, `/refresh`, and `/logout`
- `GET /api/v1/auth/me`
- `GET|POST /api/v1/tenants`
- `GET /api/v1/permissions`
- `GET /api/v1/tenants/{tenant_id}/members`
- `GET|POST /api/v1/tenants/{tenant_id}/roles`
- `GET /api/v1/tenants/{tenant_id}/audit-events`
- `POST /api/v1/authorization/check`

The refresh credential is an HttpOnly `SameSite=Strict` cookie and is never
returned in API JSON. Production deployments must terminate HTTPS before the
API and preserve `X-Forwarded-Proto: https` so the cookie receives `Secure`.

## Embed the Go kernel

The full-stack application is optional. The authorization kernel can be used
with the in-memory adapter in tests or the PostgreSQL adapter in a host app.

```go
import (
    amsonia "github.com/willunylabs/amsonia-core"
    "github.com/willunylabs/amsonia-core/memory"
)

catalog, err := amsonia.NewCatalog([]amsonia.PermissionDefinition{
    {Key: "billing:invoice:read", Description: "Read invoices"},
    {Key: "iam:role:manage"},
    {Key: "iam:grant:manage"},
    {Key: "iam:role:assign"},
})
if err != nil {
    panic(err)
}
store := memory.NewStore()
_ = catalog
_ = store
```

See [examples/saas/main.go](examples/saas/main.go) for the complete bootstrap,
delegation, authorization, and audit flow.

## Quality gates

The main local gate does not require Docker:

```bash
make check
```

It runs Go format/vet/unit/race and reachable-vulnerability checks, plus web
install/audit/type/lint/test/build and OpenAPI validation.
To run PostgreSQL integration tests against an empty test database:

```bash
TEST_DATABASE_ADMIN_URL='postgres://db_owner@127.0.0.1:5432/amsonia_test?sslmode=disable' make postgres-check
```

The PostgreSQL tests create reusable cluster-level test roles named
`amsonia_test_runtime` and `amsonia_test_maintenance`; run them only against a
dedicated development PostgreSQL cluster.

## Security boundary

- Missing or invalid signed context yields no tenant rows.
- Runtime SQL clients cannot manufacture tenant or account-discovery bindings
  by setting PostgreSQL GUC values directly.
- The maintenance adapter requires a separate pool and can export/purge tenant
  policy data without reading identity or session tables.
- Identity and session tables are global infrastructure within one deployment;
  database runtime credentials remain a high-value secret.
- The React console is an administrative interface and should not be exposed
  without HTTPS, network controls, monitoring, and backups.

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## Core and the complete product

Amsonia Core is the reusable, auditable authorization foundation. The complete
Amsonia product adds production SaaS modules, managed upgrade paths, and broader
operations tooling. Product details and engineering notes are published at
[willuny.xyz](https://willuny.xyz).

Shared public-safe sources enter this repository only through reviewed export
manifests and recorded provenance; see [docs/source-sync.md](docs/source-sync.md).

## License

Amsonia Core is Apache-2.0. The complete Amsonia product and modules outside
this repository are licensed separately.
