# Getting started

The fastest path uses Docker Compose while preserving separate migration,
runtime, and maintenance roles. A fully manual PostgreSQL setup follows it.

## Five-minute local stack

Requirements: Docker with the Compose plugin and Go 1.25.13 or newer.

```bash
git clone https://github.com/willunylabs/amsonia-core.git
cd amsonia-core
make demo
```

On first run, the command creates `.amsonia/local.env` with owner-only file
permissions, generates independent high-entropy infrastructure credentials,
builds the full stack, and asks interactively for the one system administrator.
The administrator password is read without echo and is not stored by the
launcher.

Open `http://127.0.0.1:8080`. Re-running `make demo` is idempotent.

```bash
make demo-status
make demo-down
```

`make demo-down` preserves the PostgreSQL volume and generated credentials.
The local override binds PostgreSQL to `127.0.0.1` only so host-side examples
can connect; it does not expose the database on other network interfaces.

Continue with [the business-data RLS proof](business-data-rls.md) to verify the
tenant boundary against an application-owned invoice table.

## Manual setup

Docker is not required for the manual path.

## Requirements

- Go 1.25.13 or newer within the supported release line
- PostgreSQL 16 or newer
- Node.js 22.15 or newer and npm 10

## Create a local database

Create an empty database and two non-privileged login roles. Use passwords that
are unique to this local installation.

```bash
createdb amsonia_core
createuser --login --no-superuser --no-createdb --no-createrole --no-bypassrls --pwprompt amsonia_runtime
createuser --login --no-superuser --no-createdb --no-createrole --no-bypassrls --pwprompt amsonia_maintenance
```

Run migrations as the database owner:

```bash
export AMSONIA_MIGRATION_DSN='postgres://db_owner@127.0.0.1:5432/amsonia_core?sslmode=disable'
go run ./cmd/amsonia migrate
go run ./cmd/amsonia migration-status
```

Install the least-privileged grants with a PostgreSQL cluster administrator.
The role-provisioning script prompts for two independent binding secrets. Each
secret must contain at least 64 hexadecimal characters; generate them with:

```bash
openssl rand -hex 32
```

```bash
export AMSONIA_ROLE_ADMIN_DSN='postgres://role_admin@127.0.0.1:5432/amsonia_core?sslmode=disable'
psql "$AMSONIA_ROLE_ADMIN_DSN" \
  -v runtime_role=amsonia_runtime \
  -v maintenance_role=amsonia_maintenance \
  -f ops/configure_database_roles.sql
```

Convert the runtime secret to unpadded base64url and keep it in a secret
manager or an untracked local `.env` file. Never commit it.

```bash
export AMSONIA_DATABASE_DSN='host=127.0.0.1 port=5432 dbname=amsonia_core user=amsonia_runtime sslmode=disable'
export PGPASSWORD='YOUR_RUNTIME_ROLE_PASSWORD'
export AMSONIA_TENANT_BINDING_SECRET='YOUR_UNPADDED_BASE64URL_SECRET'
```

Create the first administrator exactly once, then start the API:

```bash
go run ./cmd/amsonia bootstrap-admin
go run ./cmd/api
```

In another terminal, start the React console:

```bash
npm --prefix web ci
VITE_API_PROXY_TARGET='http://127.0.0.1:8080' npm --prefix web run dev
```

Open `http://127.0.0.1:3000` and sign in with the bootstrap administrator.

## Configuration

| Variable | Required by | Meaning |
| --- | --- | --- |
| `AMSONIA_MIGRATION_DSN` | CLI migrate/status | Database-owner connection used only by migrations |
| `AMSONIA_ROLE_ADMIN_DSN` | Role provisioning | Operator-only role administrator connection |
| `AMSONIA_DATABASE_DSN` | API/bootstrap | Non-superuser runtime connection |
| `AMSONIA_TENANT_BINDING_SECRET` | API/bootstrap | Base64url runtime binding secret |
| `AMSONIA_HTTP_ADDR` | Optional | API listen address, default `:8080` |
| `VITE_API_PROXY_TARGET` | Web development | Vite proxy target, default `http://127.0.0.1:8080` |
| `VITE_API_URL` | Web build | Optional same-origin API path prefix |

See [.env.example](../.env.example) for safe placeholders.

## PostgreSQL integration checks

Run the main local checks without Docker:

```bash
make check
```

Run PostgreSQL integration tests only against a dedicated development database:

```bash
TEST_DATABASE_ADMIN_URL='postgres://db_owner@127.0.0.1:5432/amsonia_test?sslmode=disable' \
  make postgres-check
```

The tests create reusable cluster-level roles and truncate Amsonia tables. Do
not point them at production.
