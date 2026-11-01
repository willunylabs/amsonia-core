# Casbin authorization

Amsonia Core uses [Casbin](https://casbin.apache.org/docs/overview/) v3 to
evaluate resource access and administrative grant coverage. The public Go
constructors, `CheckRequest`, `Decision`, HTTP API, and PostgreSQL schema retain
their existing contracts.

## Model and policy ownership

[`casbin_model.conf`](../casbin_model.conf) is embedded in the Go binary. A
`SyncedEnforcer` is initialized once per `Authorizer` or `Manager` and shared
across concurrent calls. A deployment does not need a model file on disk.

Tenant-scoped adapters remain the authoritative RBAC policy store. They resolve
role assignments into `EffectiveGrant` values for one subject and exact
permission. Casbin receives each validated grant and the resource/membership
facts as ABAC attributes. This combines existing RBAC policy storage with
[Casbin's attribute matching](https://casbin.apache.org/docs/abac/).

The enforcer stores no tenant policies or authorization results. Each check
reads current grants; each administrative check reads them inside the current
serialized tenant transaction. A committed revocation is visible to subsequent
checks without a policy reload, watcher, or cache invalidation. Checks already
in flight retain the existing adapter transaction semantics.

There is no additional `casbin_rule` table or Casbin role hierarchy. Policy
changes continue to use `Manager`, preserving immutable role versions,
optimistic concurrency, grant-cycle protection, last-administrator protection,
and mutation audit. Direct Casbin policy mutation is not exposed.

## Resource checks

The model's `m` matcher evaluates the exact permission, tenant boundary, and
grant scope:

| Scope | Conditions |
| --- | --- |
| `tenant` | The resource tenant, when supplied, equals the principal tenant. |
| `workspace` | A resource/create check has a workspace and an authoritative membership. Any workspace-role constraint must match that membership. |
| `own` | A resource/create check has an owner equal to the principal subject. |

Workspace-role matching retains whitespace trimming and case-insensitive
comparison. An unconstrained workspace grant still requires membership.
Only tenant scope can authorize `tenant_action`, even if owner or workspace
facts are supplied.

Workspace membership is loaded lazily at most once per `Authorizer.Check`,
including a missing-membership result. All candidate grants in that call use
the same snapshot. A subsequent check loads fresh membership, so a role change
or removal takes effect without a shared cache. Checks resolved by tenant
scope or rejected before workspace evaluation do not query membership.

`Authorizer.Check` validates the request and all loaded grants before evaluating
any candidate. Cross-tenant resource requests are rejected before reading
policy. Casbin also checks the resource tenant in its matcher. Candidate scope
order remains `tenant`, `workspace`, `own`; the first allow determines
`EffectiveScope`.

Casbin determines allow/deny. The Go wrapper maps denials to the existing
reason codes and retains their precedence. Membership or enforcement failures
return an error and a denied `dependency_unavailable` decision; malformed
persisted grants produce `invalid_policy`.

## Administrative checks

The model's `m2` matcher evaluates exact permissions and scope/constraint
coverage inside `Manager`'s existing tenant transaction:

- Management controls require the requested scope exactly (tenant scope for
  the built-in management operations).
- When delegating a grant, tenant scope covers all three supported scopes.
  Workspace and own scope each cover only themselves.
- Unconstrained workspace roles cover any target constraint. A constrained
  list must cover every target role and cannot delegate an unconstrained list.

All effective grants are validated before administrative evaluation too. A
malformed grant returns `ErrInvalidPolicy` and rolls back the operation, even
if another loaded grant would allow it. Casbin enforcement errors wrap
`ErrDependencyUnavailable` and also abort the transaction.

## Host integration

Existing integrations continue to construct `NewAuthorizer` and `NewManager`.
See [`examples/saas`](../examples/saas) for the complete host wiring. The host
still authenticates the principal, binds the active tenant, loads authoritative
resource facts, and supplies `WorkspaceMembershipReader`. Request bodies must
not replace those authoritative facts.

PostgreSQL signed tenant bindings and forced row-level security continue to
enforce storage isolation independently of the Casbin decision. See
[`business-data-rls.md`](business-data-rls.md) for application-owned tables.

## Validation

Run `make go-check` for formatting, vet, unit tests, race detection, and the
reachable-vulnerability scan. The regression suite covers the three scopes,
delegation coverage, grant validation before allow, enforcement failures,
immediate permission/role revocation, and concurrent checks with reused
subject/role IDs across tenants. Workspace regressions also cover one lookup
per check, consistent candidate facts, and fresh membership after changes.

PostgreSQL lifecycle and RLS checks additionally run with
`go test -tags postgres -count=1 ./postgres/` against an isolated test database
as described in [`getting-started.md`](getting-started.md).
