-- Configure least-privileged PostgreSQL roles for Amsonia Core.
--
-- Required psql variables:
--   runtime_role, maintenance_role
--   runtime_secret_hex, maintenance_secret_hex (at least 64 hex characters)
--
-- Roles must already exist. This script deliberately does not handle their
-- login passwords, so credentials never need to appear in source control or
-- shell history.

\set ON_ERROR_STOP on

\if :{?runtime_secret_hex}
\else
\prompt 'Runtime binding secret (64+ hex characters): ' runtime_secret_hex
\endif
\if :{?maintenance_secret_hex}
\else
\prompt 'Maintenance binding secret (64+ hex characters): ' maintenance_secret_hex
\endif

BEGIN;

ALTER ROLE :"runtime_role" NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT;
ALTER ROLE :"maintenance_role" NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT;

-- NOINHERIT only disables automatic privilege inheritance. A login role can
-- still SET ROLE to any role granted to it, so remove every pre-existing
-- membership before installing the least-privileged grants below.
SELECT format('REVOKE %I FROM %I', granted.rolname, member.rolname)
FROM pg_auth_members membership
JOIN pg_roles granted ON granted.oid = membership.roleid
JOIN pg_roles member ON member.oid = membership.member
WHERE member.rolname IN (:'runtime_role', :'maintenance_role')
\gexec

REVOKE ALL PRIVILEGES ON SCHEMA amsonia FROM :"runtime_role", :"maintenance_role";
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA amsonia FROM :"runtime_role", :"maintenance_role";
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA amsonia FROM :"runtime_role", :"maintenance_role";
REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA amsonia FROM :"runtime_role", :"maintenance_role";

GRANT USAGE ON SCHEMA amsonia TO :"runtime_role", :"maintenance_role";

-- Normal API and identity-session persistence.
GRANT SELECT, INSERT, UPDATE, DELETE ON
    amsonia.roles,
    amsonia.role_permission_grants,
    amsonia.subject_roles,
    amsonia.grant_edges,
    amsonia.tenant_memberships,
    amsonia.member_invitations
TO :"runtime_role";

GRANT SELECT, INSERT ON amsonia.role_versions, amsonia.audit_events TO :"runtime_role";
GRANT SELECT, INSERT, UPDATE ON
    amsonia.tenant_state,
    amsonia.accounts,
    amsonia.system_administrators,
    amsonia.access_sessions,
    amsonia.refresh_sessions
TO :"runtime_role";
GRANT INSERT ON amsonia.tenants TO :"runtime_role";
GRANT USAGE, SELECT ON SEQUENCE amsonia.audit_events_id_seq TO :"runtime_role";
GRANT SELECT ON amsonia.schema_migrations TO :"runtime_role";

GRANT EXECUTE ON FUNCTION amsonia.tenant_id() TO :"runtime_role";
GRANT EXECUTE ON FUNCTION amsonia.bind_tenant(TEXT, BIGINT, TEXT, TEXT) TO :"runtime_role";
GRANT EXECUTE ON FUNCTION amsonia.tenant_visible(TEXT) TO :"runtime_role";
GRANT EXECUTE ON FUNCTION amsonia.actor_id() TO :"runtime_role";
GRANT EXECUTE ON FUNCTION amsonia.bind_actor(TEXT, BIGINT, TEXT, TEXT) TO :"runtime_role";
GRANT EXECUTE ON FUNCTION amsonia.list_account_tenants() TO :"runtime_role";
GRANT EXECUTE ON FUNCTION amsonia.resolve_invitation(BYTEA) TO :"runtime_role";
GRANT EXECUTE ON FUNCTION amsonia.activate_tenant() TO :"runtime_role";
GRANT EXECUTE ON FUNCTION amsonia.fail_created_tenant(TEXT) TO :"runtime_role";

-- Offline export/purge role. It receives no identity or session access.
GRANT SELECT, DELETE ON
    amsonia.roles,
    amsonia.role_permission_grants,
    amsonia.subject_roles,
    amsonia.grant_edges,
    amsonia.role_versions,
    amsonia.audit_events
TO :"maintenance_role";
GRANT SELECT, INSERT, UPDATE ON amsonia.tenant_state TO :"maintenance_role";
GRANT SELECT, INSERT ON amsonia.purge_ledger TO :"maintenance_role";
GRANT EXECUTE ON FUNCTION amsonia.tenant_id() TO :"maintenance_role";
GRANT EXECUTE ON FUNCTION amsonia.bind_tenant(TEXT, BIGINT, TEXT, TEXT) TO :"maintenance_role";

INSERT INTO amsonia.runtime_secrets (role_name, secret, rotated_at)
VALUES
    (:'runtime_role', decode(:'runtime_secret_hex', 'hex'), now()),
    (:'maintenance_role', decode(:'maintenance_secret_hex', 'hex'), now())
ON CONFLICT (role_name) DO UPDATE
SET secret = EXCLUDED.secret, rotated_at = EXCLUDED.rotated_at;

COMMIT;
