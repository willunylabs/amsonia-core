DROP FUNCTION IF EXISTS amsonia.resolve_invitation(BYTEA);
DROP FUNCTION IF EXISTS amsonia.list_account_tenants();
DROP FUNCTION IF EXISTS amsonia.bind_actor(TEXT, BIGINT, TEXT, TEXT);
DROP FUNCTION IF EXISTS amsonia.actor_id();

DROP TABLE IF EXISTS amsonia.member_invitations;
DROP TABLE IF EXISTS amsonia.tenant_memberships;
DROP TABLE IF EXISTS amsonia.tenants;
DROP TABLE IF EXISTS amsonia.refresh_sessions;
DROP TABLE IF EXISTS amsonia.access_sessions;
DROP TABLE IF EXISTS amsonia.system_administrators;
DROP TABLE IF EXISTS amsonia.accounts;
DROP FUNCTION IF EXISTS amsonia.tenant_visible(TEXT);
