# Security model

Amsonia Core treats tenant isolation and delegated authorization as security
boundaries, not only application conventions.

- Missing or invalid signed context yields no tenant rows.
- Runtime SQL clients cannot manufacture tenant or account-discovery bindings
  by setting PostgreSQL GUC values directly.
- PostgreSQL uses forced row-level security for tenant-scoped data.
- The maintenance adapter uses a separate pool and can export or purge tenant
  policy data without reading identity or session tables.
- Identity and session tables are global infrastructure within one deployment;
  runtime database credentials remain high-value secrets.
- The React console is an administrative interface and should be protected by
  HTTPS, network controls, monitoring, and backups.

For vulnerability reporting, follow [SECURITY.md](../SECURITY.md).
