-- Amsonia PostgreSQL schema v1 rollback
-- Drops all Amsonia objects in dependency order.

DROP TABLE IF EXISTS amsonia.purge_ledger;
DROP TABLE IF EXISTS amsonia.tenant_state;
DROP TABLE IF EXISTS amsonia.audit_events;
DROP TABLE IF EXISTS amsonia.role_versions;
DROP TABLE IF EXISTS amsonia.grant_edges;
DROP TABLE IF EXISTS amsonia.subject_roles;
DROP TABLE IF EXISTS amsonia.role_permission_grants;
DROP TABLE IF EXISTS amsonia.roles;

DROP FUNCTION IF EXISTS amsonia.set_tenant(TEXT);
DROP FUNCTION IF EXISTS amsonia.tenant_id();

DROP SCHEMA IF EXISTS amsonia;
