-- Restore the previous context verification exception handling.
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
