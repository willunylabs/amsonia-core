-- Amsonia PostgreSQL security hardening v2.
--
-- Runtime tenant identity is a signed transaction-local binding. The runtime
-- role may choose a tenant ID only when the application also supplies an HMAC
-- generated with a secret that is unavailable to SQL clients. The signature
-- covers the tenant, database role, transaction ID, and a single-use nonce.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS amsonia.runtime_secrets (
    role_name   NAME        PRIMARY KEY,
    secret      BYTEA       NOT NULL CHECK (octet_length(secret) >= 32),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

REVOKE ALL ON TABLE amsonia.runtime_secrets FROM PUBLIC;

CREATE OR REPLACE FUNCTION amsonia.tenant_id()
RETURNS TEXT
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, amsonia
AS $$
DECLARE
    bound_tenant TEXT := NULLIF(current_setting('amsonia.tenant_id', TRUE), '');
    bound_txid TEXT := NULLIF(current_setting('amsonia.tenant_txid', TRUE), '');
    bound_nonce TEXT := NULLIF(current_setting('amsonia.tenant_nonce', TRUE), '');
    bound_signature TEXT := NULLIF(current_setting('amsonia.tenant_signature', TRUE), '');
    signing_secret BYTEA;
    expected BYTEA;
BEGIN
    IF bound_tenant IS NULL OR bound_txid IS NULL OR bound_nonce IS NULL OR bound_signature IS NULL THEN
        RETURN NULL;
    END IF;
    IF bound_txid <> txid_current()::TEXT OR bound_nonce !~ '^[0-9a-f]{32}$' OR
       bound_signature !~ '^[0-9a-f]{64}$' THEN
        RETURN NULL;
    END IF;
    SELECT secret INTO signing_secret
      FROM amsonia.runtime_secrets
     WHERE role_name = session_user;
    IF signing_secret IS NULL THEN
        RETURN NULL;
    END IF;
    expected := public.hmac(
        convert_to(bound_tenant || E'\n' || session_user || E'\n' || bound_txid || E'\n' || bound_nonce, 'UTF8'),
        signing_secret,
        'sha256'
    );
    IF decode(bound_signature, 'hex') <> expected THEN
        RETURN NULL;
    END IF;
    RETURN bound_tenant;
EXCEPTION
    WHEN OTHERS THEN
        RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION amsonia.bind_tenant(
    p_tenant_id TEXT,
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
    IF p_tenant_id IS NULL OR p_tenant_id = '' OR length(p_tenant_id) > 256 THEN
        RAISE EXCEPTION 'invalid tenant binding';
    END IF;
    IF p_txid <> txid_current() OR p_nonce !~ '^[0-9a-f]{32}$' THEN
        RAISE EXCEPTION 'invalid tenant binding';
    END IF;
    SELECT secret INTO signing_secret
      FROM amsonia.runtime_secrets
     WHERE role_name = session_user;
    IF signing_secret IS NULL THEN
        RAISE EXCEPTION 'tenant binding is not configured for role';
    END IF;
    expected := public.hmac(
        convert_to(p_tenant_id || E'\n' || session_user || E'\n' || p_txid::TEXT || E'\n' || p_nonce, 'UTF8'),
        signing_secret,
        'sha256'
    );
    IF p_signature IS NULL OR p_signature !~ '^[0-9a-f]{64}$' THEN
        RAISE EXCEPTION 'invalid tenant binding';
    END IF;
    IF decode(p_signature, 'hex') <> expected THEN
        RAISE EXCEPTION 'invalid tenant binding';
    END IF;
    PERFORM set_config('amsonia.tenant_id', p_tenant_id, TRUE);
    PERFORM set_config('amsonia.tenant_txid', p_txid::TEXT, TRUE);
    PERFORM set_config('amsonia.tenant_nonce', p_nonce, TRUE);
    PERFORM set_config('amsonia.tenant_signature', p_signature, TRUE);
END;
$$;

ALTER FUNCTION amsonia.tenant_id() OWNER TO CURRENT_USER;
ALTER FUNCTION amsonia.bind_tenant(TEXT, BIGINT, TEXT, TEXT) OWNER TO CURRENT_USER;

REVOKE ALL ON FUNCTION amsonia.set_tenant(TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION amsonia.tenant_id() FROM PUBLIC;
REVOKE ALL ON FUNCTION amsonia.bind_tenant(TEXT, BIGINT, TEXT, TEXT) FROM PUBLIC;

-- Audit rows are purged through a separately authorized maintenance path.
DROP POLICY IF EXISTS p_ae_delete ON amsonia.audit_events;
CREATE POLICY p_ae_delete ON amsonia.audit_events
    FOR DELETE USING (amsonia.tenant_id() = tenant_id);

-- The permanent replay ledger is tenant scoped and append only. Deployments
-- must still revoke all ledger access from the normal runtime role and grant
-- SELECT/INSERT only to the maintenance role.
ALTER TABLE amsonia.purge_ledger ENABLE ROW LEVEL SECURITY;
ALTER TABLE amsonia.purge_ledger FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS p_pl_select ON amsonia.purge_ledger;
DROP POLICY IF EXISTS p_pl_insert ON amsonia.purge_ledger;
CREATE POLICY p_pl_select ON amsonia.purge_ledger
    FOR SELECT USING (amsonia.tenant_id() = tenant_id);
CREATE POLICY p_pl_insert ON amsonia.purge_ledger
    FOR INSERT WITH CHECK (amsonia.tenant_id() = tenant_id);

-- Role-version history is immutable after insertion.
DROP POLICY IF EXISTS p_rv_update ON amsonia.role_versions;
REVOKE UPDATE, DELETE ON amsonia.role_versions FROM PUBLIC;
