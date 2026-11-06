# Default organization (issue #71)

The standalone application defaults to `AMSONIA_TENANCY_MODE=single`. The embedded authorization kernel and `NewService` composition retain their tenant contracts. Standalone entrypoints use an explicitly configured application service.

In single mode an operator can select an existing active tenant with `AMSONIA_ORGANIZATION_ID`; otherwise exactly one active tenant is used. A narrow PostgreSQL function resolves the ID across all tenants without granting runtime access to the tenant directory. Multiple active tenants without an explicit ID fail startup and readiness. No active tenant is allowed before first administrator setup. Pending and failed tenants are never selected.

Administrator bootstrap prepares the organization using existing tenant bootstrap, owner grants and audit behavior. `amsonia setup-organization` resumes incomplete setup and is idempotent. A dedicated short-lived database connection holds a session advisory lock while setup runs, including when the service pool has only one connection. Concurrent setup commands serialize; the kernel's multi-transaction tenant activation protocol is preserved.

The service filters membership discovery, rejects tenant creation and checks the selected organization before tenant-bound HTTP access, authorization checks and invitation preview/acceptance. Membership and permission checks still apply. An authenticated application configuration endpoint drives the UI. Single mode shows organization identity, hides tenant creation/switching and provides an operator-directed empty state. Multi mode preserves existing workflows.

Mode and organization selection are deployment configuration, not a browser preference. Changing mode requires restarting all API replicas with consistent settings. Migration 8 and the updated role grants must be applied before upgrading the API. Operators upgrading an existing multi-tenant installation must explicitly enable `multi` or select an existing organization; no data is moved or deleted.

Validation covers configuration parsing, real least-privilege PostgreSQL selection and bypass attempts, concurrent/repeated setup, frontend mode-specific behavior, and browser flows in both modes.

Release plan: ship this as an application preview behavior change with migration 8, the new authenticated configuration endpoint and explicit upgrade instructions. No exported kernel symbols or interfaces change. Multi mode retains the existing tenant API, role naming and administration behavior.
