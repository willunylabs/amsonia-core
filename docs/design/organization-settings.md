# Organization settings (issue #73)

The standalone console needs a dedicated organization identity form after setup.
All active members may read the selected organization's name, ID, state,
creation/update times and settings version. Editing its name is a registry
operation reserved for active system administrators with active membership.
Tenant roles retain their existing seven permissions and grant semantics.

GET/PATCH `/api/v1/tenants/{tenant_id}/settings` run behind the existing tenant
middleware, including single-organization selection. PATCH accepts only `name`
and `expected_version`. Migration 9 adds a version column and two narrowly scoped
security-definer functions. Each uses the signed tenant binding. The update
function also checks account status, administrator status and membership. Runtime
receives EXECUTE only; global tenant SELECT/UPDATE privileges are not added.

The update holds the normal tenant mutation lock, compares the version, updates
the name/version/time and writes `organization.update` audit in one transaction.
Unchanged names emit a noop audit without advancing the version. Missing tenant,
forbidden actor and stale version are rejected. A rolled-back audit rolls back
the name update. Names are trimmed, valid UTF-8, contain no control characters
and occupy at most 80 bytes, consistently across creation/configuration/updates.

The full-page form has independent loading, retry, save/cancel, success and
conflict states. Conflicts require an explicit reload before another submission;
writes are never automatically replayed. Switching organizations discards the
old form. Late responses from an unmounted form cannot replace a new
organization's state or trigger its workspace refresh.

Release plan: additive preview application endpoint/UI plus migration 9. Apply
the migration and refreshed runtime role grants before starting new binaries.
Existing names are preserved; version starts at one. No exported kernel APIs or
interfaces change. Downgrade removes the settings functions and version column.
