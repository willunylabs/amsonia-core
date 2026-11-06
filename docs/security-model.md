# Security model

Amsonia Core treats tenant isolation and delegated authorization as security
boundaries, not only application conventions.

- Missing or invalid signed context yields no tenant rows.
- Runtime SQL clients cannot manufacture tenant or account-discovery bindings
  by setting PostgreSQL GUC values directly.
- PostgreSQL uses forced row-level security for tenant-scoped data.
- Typed Go predicates evaluate resource access and administrative grant coverage
  from validated, tenant-scoped grants without a decision cache; see the
  [authorization contract](authorization.md).
- Core mutations recheck active tenant membership inside the serialized policy
  transaction. An active member must retain all four tenant administration
  controls, including member management. Suspended administrators do not count.
- The maintenance adapter uses a separate pool and can export or purge tenant
  policy data without reading identity or session tables.
- Identity and session tables are global infrastructure within one deployment;
  runtime database credentials remain high-value secrets.
- The React console is an administrative interface and should be protected by
  HTTPS, network controls, monitoring, and backups.

Maintenance exports retain the `amsonia.tenant.v1` payload: roles, scoped grants,
role-version snapshots, grant edges, and mutation audit. Both adapters serialize
policy collections in the same complete order: role ID; grant role, permission,
scope, and workspace-role list; role ID and numeric version; and edge grantor,
grantee, and role. Identifiers use bytewise ordering independent of database
collation. Grant sets inside version snapshots follow the same grant ordering.
Audit keeps its recorded sequence, and serialization does not modify stored
snapshots or normalize field values.

For vulnerability reporting, follow [SECURITY.md](../SECURITY.md).
