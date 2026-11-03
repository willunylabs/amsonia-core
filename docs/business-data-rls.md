# Protecting host business data with Amsonia's tenant boundary

Amsonia Core's PostgreSQL binding is reusable by host-application tables. This
is useful when authorization decisions live in Core but invoices, projects, or
other tenant-owned rows remain in the application's own schema.

This is a storage-isolation primitive, not an authorization shortcut. The host
application still authenticates the caller, loads the resource, and checks the
required Amsonia permission before reading or mutating business data.

## The invariant

The runtime connection must be a non-superuser, must not own protected tables,
and must not have `BYPASSRLS`. Every operation runs through
`postgres.Store.RunTenant`, which installs a signed, transaction-local tenant
binding. The table policy compares its `tenant_id` to
`amsonia.tenant_id()`:

```sql
ALTER TABLE app.invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE app.invoices FORCE ROW LEVEL SECURITY;

CREATE POLICY invoice_tenant_isolation ON app.invoices
    USING (tenant_id = amsonia.tenant_id())
    WITH CHECK (tenant_id = amsonia.tenant_id());
```

Both `USING` and `WITH CHECK` matter: the first filters existing rows and the
second rejects writes that claim a different tenant. `FORCE ROW LEVEL
SECURITY` also subjects the table owner to its policies, but the running API
must still use a separate non-owner role.

## Run the proof

Start the local stack:

```bash
make demo
```

Apply the example table as the database owner:

```bash
docker compose --env-file .amsonia/local.env \
  --file compose.yaml --file compose.local.yaml exec -T postgres \
  psql --username amsonia_owner --dbname amsonia_core \
  --set=runtime_role=amsonia_runtime \
  --file=/dev/stdin < examples/business-data-rls/schema.sql
```

Run the Go example with the generated runtime DSN and binding secret. The
environment file is owner-readable only and ignored by Git; do not copy it or
its contents into source control. The example writes one invoice for each of
two tenants, queries without a tenant `WHERE` clause, and attempts a
cross-tenant insert:

```bash
set -a
. .amsonia/local.env
set +a
go run ./examples/business-data-rls
```

The expected result is one visible invoice per tenant and a PostgreSQL RLS
rejection for the cross-tenant insert. The PostgreSQL-tagged integration test
also proves that forged session settings expose zero rows:

```bash
make postgres-check TEST_DATABASE_ADMIN_URL='postgres://...'
```

The complete illustrative schema is
[`examples/business-data-rls/schema.sql`](../examples/business-data-rls/schema.sql).
Copy the pattern into an application-owned migration and grant only the exact
table operations the runtime needs.

## Do not weaken the boundary

- Do not accept a tenant ID from a request and issue raw SQL outside
  `RunTenant`.
- Do not make the API role a table owner, superuser, or `BYPASSRLS` role.
- Do not rely on RLS as the permission decision; run the Amsonia authorization
  check first.
- Do not expose the binding secret to browser code, logs, or source control.
- Do not share the migration/owner DSN with the running API.

## Try the form in a browser

After applying the same schema and loading the same runtime environment, run:

```bash
go run ./examples/business-data-rls -serve
```

Open the exact loopback URL printed in the terminal and use one of the
generated Basic Auth credentials. Use a separate private window or browser
profile for each identity, since browsers cache Basic Auth credentials.

1. Sign in as `acme-editor`. Open **New invoice**, enter a description and
   `1250` cents, then save. The new invoice appears as **$12.50** on the list.
2. Sign in separately as `acme-viewer`. The invoice is visible, but the create
   page and a direct form submission both return 403.
3. Sign in as `globex-editor`. The Acme invoice is absent. A Globex invoice
   created here is likewise invisible to Acme.

The example serves a bounded list (`GET /`), a separate creation form
(`GET /invoices/new`), and its write endpoint (`POST /invoices`). The form
posts only a description and amount. The server resolves a demo identity and
tenant from the authenticated credentials, checks the exact Amsonia
permission, then calls `RunTenant` using that same tenant. The list query
still has no tenant predicate: PostgreSQL enforces that boundary. Successful
writes redirect to the list so reloading the page does not resubmit the form.

See [web.go](../examples/business-data-rls/web.go) for the HTTP and storage
path, [invoices.html](../examples/business-data-rls/invoices.html) for the form,
and [web_test.go](../examples/business-data-rls/web_test.go) for the denial and
validation cases. The example binds only to 127.0.0.1. Its generated
access codes and in-memory grants are development fixtures; use your own
authentication and membership/policy source in a real application. The
browser example uses distinct fixture tenant IDs and leaves its invoices in
the demo database. It does not add routes to the default console.

To adapt it, replace the invoice schema with your own table, add permissions
to the host catalog following [the SaaS example](../examples/saas/main.go),
and keep the same three checks: the right permission works, a member without
it is denied, and the other tenant's rows stay invisible. For object-specific
reads or writes, authorize against the actual resource instead of a
tenant-wide action.
