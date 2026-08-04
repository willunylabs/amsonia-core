# Amsonia

Opinionated, tenant-safe delegated authorization for Go SaaS applications —
embedded without additional infrastructure.

Amsonia is the open-source core of
[willuny-go-admin](https://github.com/willunylabs/willuny-go-admin), the
deployable SaaS starter for teams that need identity (Google OAuth), Stripe
billing, orders, Resend email, full tenant/workspace lifecycle, admin APIs, and
operations — the complete product built around this authorization kernel.

## Why Amsonia

Most authorization systems either embed a generic policy engine (Casbin) or
deploy a separate Zanzibar-style service (OpenFGA, SpiceDB). Amsonia takes a
different position:

> **Tenant-first delegated authorization, embedded directly in your Go SaaS.**

- **Tenant isolation is mandatory** — every check and mutation carries an
  explicit tenant, enforced twice in PostgreSQL (explicit predicates + forced
  RLS).
- **Scoped grants** — the same permission can be granted at `own`, `workspace`,
  or `tenant` scope, with a deterministic evaluation order.
- **Safe delegation** — an administrator can only delegate what they actually
  hold; grant cycles are impossible by construction.
- **Immutable role versions** — every role change is an append-only snapshot for
  audit reconstruction.
- **No extra infrastructure** — embed it, migrate your PostgreSQL, ship.

## What's included

| Capability | Status |
| --- | --- |
| Permission catalog (immutable, validated) | v0.1 |
| `own` / `workspace` / `tenant` scope evaluation | v0.1 |
| Deterministic decision reasons | v0.1 |
| Delegated administration with scope coverage | v0.1 |
| Grant-cycle prevention | v0.1 |
| One-time tenant bootstrap | v0.1 |
| Last-administrator protection | v0.1 |
| Immutable role versions + mutation audit | v0.1 |
| In-memory adapter (tests/dev) | v0.1 |
| PostgreSQL adapter + RLS migrations | v0.1 |
| Tenant export / purge (offline maintenance) | v0.1 |

## Quickstart

```go
package main

import (
	"context"
	"fmt"

	"github.com/willunylabs/amsonia"
	"github.com/willunylabs/amsonia/memory"
)

func main() {
	ctx := context.Background()

	// 1. Define the application's permission catalog (immutable).
	catalog, err := amsonia.NewCatalog([]amsonia.PermissionDefinition{
		{Key: "billing:invoice:read", Description: "Read invoices"},
		{Key: "iam:role:manage", Description: "Manage roles"},
		{Key: "iam:grant:manage", Description: "Manage grants"},
		{Key: "iam:role:assign", Description: "Assign roles"},
	})
	if err != nil {
		panic(err)
	}

	store := memory.NewStore()
	controls := amsonia.ControlPermissions{
		ManageRoles:  "iam:role:manage",
		ManageGrants: "iam:grant:manage",
		AssignRoles:  "iam:role:assign",
	}

	// 2. Bootstrap a tenant exactly once.
	bootstrapper, _ := amsonia.NewBootstrapper(catalog, store, controls, hostProvisioner{}, amsonia.RealClock{})
	if _, err := bootstrapper.BootstrapTenant(ctx, amsonia.BootstrapInput{
		TenantID:       "tenant-1",
		OwnerSubjectID: "owner-1",
		OwnerRoleID:    "role-owner",
		OwnerRoleName:  "tenant-owner",
		Grants: []amsonia.RolePermissionGrant{
			{RoleID: "role-owner", Permission: "iam:role:manage", Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner", Permission: "iam:grant:manage", Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner", Permission: "iam:role:assign", Scope: amsonia.ScopeTenant},
			{RoleID: "role-owner", Permission: "billing:invoice:read", Scope: amsonia.ScopeTenant},
		},
		Metadata: amsonia.MutationMetadata{ReasonCode: "tenant_provisioning"},
	}); err != nil {
		panic(err)
	}

	// 3. Authorize requests.
	authorizer, _ := amsonia.NewAuthorizer(catalog, store, noMemberships{})
	decision, err := authorizer.Check(ctx, amsonia.CheckRequest{
		Principal:  amsonia.Principal{TenantID: "tenant-1", SubjectID: "owner-1"},
		Permission: "billing:invoice:read",
		Mode:       amsonia.ResourceExisting,
		Resource:   amsonia.ResourceContext{TenantID: "tenant-1", ResourceID: "inv-42"},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("allowed:", decision.Allowed) // allowed: true
}
```

See `examples/saas` for the full bootstrap → delegate → check → audit flow.

## PostgreSQL

Apply the self-contained migration, then create a dedicated application role:

```sql
-- run migrations/000001_amsonia_base.up.sql as the migration owner
CREATE ROLE amsonia_app LOGIN PASSWORD '...';
GRANT USAGE ON SCHEMA amsonia TO amsonia_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA amsonia TO amsonia_app;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA amsonia TO amsonia_app;
```

The application role must **not** be a superuser, the table owner, or
`BYPASSRLS`. Every table is `FORCE ROW LEVEL SECURITY`; missing tenant context
denies access.

## Security model

- Missing, mismatched, malformed, unknown, stale, or dependency-failed input
  fails closed.
- No `global` scope, no wildcards, no arbitrary JSON conditions, no explicit
  deny, no role inheritance, no platform-admin bypass.
- Authorization decisions expose only `Allowed`, a bounded reason code, and the
  effective scope — never policy rows.
- Denied/conflict/failed attempts are recorded through the external
  `SecurityAuditSink` because a rolled-back transaction cannot retain them.

## Support

Community support is best effort. Report security issues privately via
[SECURITY.md](SECURITY.md).

## License

Apache-2.0. willuny-go-admin — the complete product built around this core — is
licensed separately under a commercial source license.
