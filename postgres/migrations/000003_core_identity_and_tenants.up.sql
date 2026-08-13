-- Amsonia Core full-stack identity, tenant registry, memberships, and sessions.

CREATE TABLE IF NOT EXISTS amsonia.accounts (
    account_id          TEXT        PRIMARY KEY,
    email               TEXT        NOT NULL,
    normalized_email    TEXT        NOT NULL UNIQUE,
    password_hash       TEXT        NOT NULL,
    status              TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    failed_login_count  INTEGER     NOT NULL DEFAULT 0 CHECK (failed_login_count >= 0),
    locked_until        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at       TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS amsonia.system_administrators (
    account_id    TEXT        PRIMARY KEY REFERENCES amsonia.accounts(account_id) ON DELETE CASCADE,
    appointed_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS amsonia.access_sessions (
    token_hash       BYTEA       PRIMARY KEY,
    account_id       TEXT        NOT NULL REFERENCES amsonia.accounts(account_id) ON DELETE CASCADE,
    expires_at       TIMESTAMPTZ NOT NULL,
    revoked_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    remote_address   TEXT        NOT NULL DEFAULT '',
    user_agent       TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_amsonia_access_sessions_account
    ON amsonia.access_sessions(account_id, expires_at);

CREATE TABLE IF NOT EXISTS amsonia.refresh_sessions (
    token_hash       BYTEA       PRIMARY KEY,
    account_id       TEXT        NOT NULL REFERENCES amsonia.accounts(account_id) ON DELETE CASCADE,
    expires_at       TIMESTAMPTZ NOT NULL,
    revoked_at       TIMESTAMPTZ,
    rotated_to_hash  BYTEA,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    remote_address   TEXT        NOT NULL DEFAULT '',
    user_agent       TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_amsonia_refresh_sessions_account
    ON amsonia.refresh_sessions(account_id, expires_at);

CREATE TABLE IF NOT EXISTS amsonia.tenants (
    tenant_id      TEXT        PRIMARY KEY,
    name           TEXT        NOT NULL,
    state          TEXT        NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'active', 'failed')),
    created_by     TEXT        NOT NULL REFERENCES amsonia.accounts(account_id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION amsonia.tenant_visible(p_tenant_id TEXT)
RETURNS BOOLEAN
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
DECLARE
    current_tenant TEXT;
BEGIN
    SELECT amsonia.tenant_id() INTO current_tenant;
    RETURN current_tenant IS NOT NULL AND current_tenant = p_tenant_id;
END;
$$;

REVOKE ALL ON FUNCTION amsonia.tenant_visible(TEXT) FROM PUBLIC;

CREATE TABLE IF NOT EXISTS amsonia.tenant_memberships (
    tenant_id    TEXT        NOT NULL REFERENCES amsonia.tenants(tenant_id) ON DELETE CASCADE,
    account_id   TEXT        NOT NULL REFERENCES amsonia.accounts(account_id) ON DELETE CASCADE,
    status       TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_amsonia_memberships_account
    ON amsonia.tenant_memberships(account_id, tenant_id);

CREATE TABLE IF NOT EXISTS amsonia.member_invitations (
    tenant_id       TEXT        NOT NULL REFERENCES amsonia.tenants(tenant_id) ON DELETE CASCADE,
    invitation_id   TEXT        NOT NULL,
    normalized_email TEXT       NOT NULL,
    display_email   TEXT        NOT NULL,
    token_hash      BYTEA       NOT NULL UNIQUE,
    created_by      TEXT        NOT NULL REFERENCES amsonia.accounts(account_id),
    expires_at      TIMESTAMPTZ NOT NULL,
    accepted_at     TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, invitation_id)
);
CREATE INDEX IF NOT EXISTS idx_amsonia_invitations_tenant_created
    ON amsonia.member_invitations(tenant_id, created_at DESC);

-- Memberships and invitations are tenant-owned. Tenant discovery for the
-- authenticated account uses narrowly scoped security-definer functions.
ALTER TABLE amsonia.tenant_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.tenant_memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY p_tm_select ON amsonia.tenant_memberships
    FOR SELECT USING (amsonia.tenant_visible(tenant_id));
CREATE POLICY p_tm_insert ON amsonia.tenant_memberships
    FOR INSERT WITH CHECK (amsonia.tenant_visible(tenant_id));
CREATE POLICY p_tm_update ON amsonia.tenant_memberships
    FOR UPDATE USING (amsonia.tenant_visible(tenant_id))
    WITH CHECK (amsonia.tenant_visible(tenant_id));
CREATE POLICY p_tm_delete ON amsonia.tenant_memberships
    FOR DELETE USING (amsonia.tenant_visible(tenant_id));

ALTER TABLE amsonia.member_invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.member_invitations FORCE ROW LEVEL SECURITY;
CREATE POLICY p_mi_select ON amsonia.member_invitations
    FOR SELECT USING (amsonia.tenant_visible(tenant_id));
CREATE POLICY p_mi_insert ON amsonia.member_invitations
    FOR INSERT WITH CHECK (amsonia.tenant_visible(tenant_id));
CREATE POLICY p_mi_update ON amsonia.member_invitations
    FOR UPDATE USING (amsonia.tenant_visible(tenant_id))
    WITH CHECK (amsonia.tenant_visible(tenant_id));
CREATE POLICY p_mi_delete ON amsonia.member_invitations
    FOR DELETE USING (amsonia.tenant_visible(tenant_id));

CREATE OR REPLACE FUNCTION amsonia.actor_id()
RETURNS TEXT
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
DECLARE
    bound_actor TEXT := NULLIF(current_setting('amsonia.actor_id', TRUE), '');
    bound_txid TEXT := NULLIF(current_setting('amsonia.actor_txid', TRUE), '');
    bound_nonce TEXT := NULLIF(current_setting('amsonia.actor_nonce', TRUE), '');
    bound_signature TEXT := NULLIF(current_setting('amsonia.actor_signature', TRUE), '');
    signing_secret BYTEA;
    expected BYTEA;
BEGIN
    IF bound_actor IS NULL OR bound_txid IS NULL OR bound_nonce IS NULL OR bound_signature IS NULL THEN
        RETURN NULL;
    END IF;
    IF bound_txid <> txid_current()::TEXT OR bound_nonce !~ '^[0-9a-f]{32}$' OR
       bound_signature !~ '^[0-9a-f]{64}$' THEN
        RETURN NULL;
    END IF;
    SELECT secret INTO signing_secret FROM amsonia.runtime_secrets WHERE role_name = session_user;
    IF signing_secret IS NULL THEN
        RETURN NULL;
    END IF;
    expected := public.hmac(
        convert_to('actor' || E'\n' || bound_actor || E'\n' || session_user || E'\n' || bound_txid || E'\n' || bound_nonce, 'UTF8'),
        signing_secret,
        'sha256'
    );
    IF decode(bound_signature, 'hex') <> expected THEN
        RETURN NULL;
    END IF;
    RETURN bound_actor;
EXCEPTION WHEN OTHERS THEN
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION amsonia.bind_actor(
    p_account_id TEXT,
    p_txid BIGINT,
    p_nonce TEXT,
    p_signature TEXT
)
RETURNS VOID
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
DECLARE
    signing_secret BYTEA;
    expected BYTEA;
BEGIN
    IF p_account_id IS NULL OR p_account_id = '' OR length(p_account_id) > 128 OR p_account_id ~ E'[\r\n]' THEN
        RAISE EXCEPTION 'invalid actor binding';
    END IF;
    IF p_txid <> txid_current() OR p_nonce !~ '^[0-9a-f]{32}$' OR
       p_signature IS NULL OR p_signature !~ '^[0-9a-f]{64}$' THEN
        RAISE EXCEPTION 'invalid actor binding';
    END IF;
    SELECT secret INTO signing_secret FROM amsonia.runtime_secrets WHERE role_name = session_user;
    IF signing_secret IS NULL THEN
        RAISE EXCEPTION 'actor binding is not configured for role';
    END IF;
    expected := public.hmac(
        convert_to('actor' || E'\n' || p_account_id || E'\n' || session_user || E'\n' || p_txid::TEXT || E'\n' || p_nonce, 'UTF8'),
        signing_secret,
        'sha256'
    );
    IF decode(p_signature, 'hex') <> expected THEN
        RAISE EXCEPTION 'invalid actor binding';
    END IF;
    PERFORM set_config('amsonia.actor_id', p_account_id, TRUE);
    PERFORM set_config('amsonia.actor_txid', p_txid::TEXT, TRUE);
    PERFORM set_config('amsonia.actor_nonce', p_nonce, TRUE);
    PERFORM set_config('amsonia.actor_signature', p_signature, TRUE);
END;
$$;

CREATE OR REPLACE FUNCTION amsonia.list_account_tenants()
RETURNS TABLE(tenant_id TEXT, name TEXT, state TEXT, created_at TIMESTAMPTZ)
LANGUAGE SQL
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
    SELECT t.tenant_id, t.name, t.state, t.created_at
      FROM amsonia.tenants t
      JOIN amsonia.tenant_memberships m ON m.tenant_id = t.tenant_id
     WHERE m.account_id = amsonia.actor_id()
       AND m.status = 'active'
       AND t.state = 'active'
     ORDER BY t.created_at, t.tenant_id
$$;

CREATE OR REPLACE FUNCTION amsonia.resolve_invitation(p_token_hash BYTEA)
RETURNS TABLE(tenant_id TEXT, invitation_id TEXT)
LANGUAGE SQL
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
    SELECT i.tenant_id, i.invitation_id
      FROM amsonia.member_invitations i
     WHERE i.token_hash = p_token_hash
       AND i.accepted_at IS NULL
       AND i.revoked_at IS NULL
       AND i.expires_at > now()
$$;

REVOKE ALL ON FUNCTION amsonia.actor_id() FROM PUBLIC;
REVOKE ALL ON FUNCTION amsonia.bind_actor(TEXT, BIGINT, TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION amsonia.list_account_tenants() FROM PUBLIC;
REVOKE ALL ON FUNCTION amsonia.resolve_invitation(BYTEA) FROM PUBLIC;
