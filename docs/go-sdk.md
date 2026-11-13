# Embed the Go SDK

Use the SDK when your application owns sign-in and business data and needs
tenant-aware authorization. The example uses the in-memory adapter to exercise
the kernel without a database or running Console.

## Run from your own module

With Go 1.26.11 or newer, run these commands from a directory where you want to
create a disposable example. They use the published preview.7 module and the
example included in that version.

```bash
mkdir amsonia-sdk-example
cd amsonia-sdk-example
export GOWORK=off
go mod init example.com/amsonia-sdk-example
go get github.com/willunylabs/amsonia-core@v0.2.0-preview.7
sdk_dir="$(go list -m -f '{{.Dir}}' github.com/willunylabs/amsonia-core)"
cp "$sdk_dir/examples/saas/main.go" ./main.go
go mod tidy
go run .
```

The example bootstraps a tenant, assigns Alice an invoice reader role, and
prints allow and deny decisions. Alice can read invoice `inv-1` and receives a
denial for writing it. The final privilege-escalation attempt is also denied.

## Put the check on a request path

The example's `authorizer.Check` call takes the authenticated subject and tenant,
the exact permission from your catalog, and resource facts read by your
application. Use the resource's tenant, owner and workspace from trusted data.
Tenant-wide actions use `ResourceTenantAction`. A workspace grant uses a
`WorkspaceMembershipReader` backed by your application's membership data.
Proceed with a protected operation only after a successful check with
`decision.Allowed` set to true. See the [authorization contract](authorization.md)
for the scope and delegation rules.

When managing permissions through `Manager`, provide your own durable
`SecurityAuditSink`. The example's audit writer and bootstrap authorizer are
fixtures for local evaluation.

## Use PostgreSQL for persistent policy

Apply the [database setup](getting-started.md#manual-setup) using an operator
connection. Build a pool for the provisioned non-owner runtime role and pass it
with the matching decoded tenant binding secret to `postgres.NewStore`.
Supply that store to the same bootstrapper, manager and authorizer constructors.
The [business-data RLS example](business-data-rls.md) shows how to use the
runtime store with application-owned tables and verify cross-tenant isolation.
