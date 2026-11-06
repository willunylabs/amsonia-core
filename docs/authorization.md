# Authorization contract

Core evaluates its fixed authorization model with typed Go predicates in
`policy.go`. No external policy language, enforcer cache, or policy mirror is
required. The public kernel constructors, permission vocabulary, store contract,
role snapshots, and export format are unchanged.

## Access checks

The application owns an exact `namespace:resource:action` permission catalog.
Every effective grant is validated before evaluating any candidate. Invalid
policy fails closed even when another row could allow access.

| Scope | Required resource facts |
| --- | --- |
| `tenant` | Principal and resource belong to the same tenant |
| `own` | Same tenant and nonempty owner equals the principal |
| `workspace` | Same tenant and authoritative workspace membership; constrained roles match case-insensitively after trimming |

A tenant action requires a tenant grant. Own or workspace grants cannot authorize
administrative collection endpoints. Workspace checks use the host's membership
reader; the standalone Core API has no workspace domain and returns no such
membership. Hosts supply resource owner/workspace facts from authoritative data.

Delegation requires the exact permission and sufficient scope. Tenant authority
covers narrower scopes; own authority covers own grants; workspace authority
covers workspace grants whose role constraints are a subset of the actor's.
An unconstrained workspace grant covers all workspace roles. Mutation constraints
retain their canonical, case-sensitive set comparison used by the existing
kernel contract. Unknown scopes or permissions fail closed.

## Administration boundaries

Manager serializes every policy mutation per tenant. Expected role versions
protect edits and grants against stale writes. Immutable snapshots describe each
role version. Assignment checks actual grant coverage and rejects delegation
cycles. Role retirement requires no grants or subject bindings.

The generic kernel preserves at least one subject holding its three configured
control permissions. Core's internal PostgreSQL host adds active tenant membership
and member-management authority: at least one active member must hold all four
`iam:role:manage`, `iam:grant:manage`, `iam:role:assign`, and `iam:member:manage`
permissions at tenant scope. These checks run in the same locked transaction as
the policy change. Member suspension/removal uses the same tenant lock.
Core reads the relevant active-member control grants in one batch and validates
them before accepting any administrator. The number of permission queries does
not grow with ordinary membership; malformed control grants still fail closed.

HTTP grant changes also require `iam:grant:manage`; possessing the permission
being delegated alone does not open the grant-management UI or API. System
administrators may create tenants, but receive no implicit tenant-policy bypass.
The bootstrap owner explicitly receives the seven catalog permissions.

PostgreSQL signed transaction bindings and forced RLS remain independent storage
boundaries. Successful mutations and invitations/member changes append tenant
audit records. Failed kernel mutations are recorded after rollback with generated
request IDs. See [security model](security-model.md).
